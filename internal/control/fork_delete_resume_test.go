package control

// Reproduction of the reported bug: fork, delete the forked session, return to
// the source session and fork again. Verifies the source session's checkpoints
// and boundaries survive the fork/delete cycle, and that a fork from a two-turn
// session keeps both turns.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

func TestForkThenDeleteThenResumeThenForkAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-a.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	runner := &recordingSessionRunner{session: sess}
	c := New(Options{
		Runner:      runner,
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
	})

	o := newTurnOrchestrator(c)
	if err := o.runTurnWithRawDisplay(context.Background(), "first turn", "first turn", ""); err != nil {
		t.Fatal(err)
	}
	if !c.CheckpointHasBoundary(0) {
		t.Fatal("boundary 0 missing after first turn")
	}

	// Fork at turn 0 -> switches to the new session B
	forkPath, err := c.ForkNamed(0, "branch")
	if err != nil {
		t.Fatalf("fork at turn 0: %v", err)
	}
	t.Logf("forked session path: %s", forkPath)

	// The branch must inherit the source checkpoints (otherwise the branch
	// renumbers turns from 0 and cannot fork/rewind immediately).
	forkCkpt := store.SessionCheckpointDir(forkPath)
	ents, err := os.ReadDir(forkCkpt)
	if err != nil || len(ents) == 0 {
		t.Fatalf("fork branch checkpoint dir %s: err=%v entries=%d, want inherited turn-0.json", forkCkpt, err, len(ents))
	}
	cps := c.Checkpoints()
	t.Logf("branch checkpoints after fork: %+v", cps)
	if len(cps) != 1 || cps[0].Turn != 0 {
		t.Fatalf("branch checkpoints = %+v, want inherited turn 0", cps)
	}
	// Forking again inside the fresh branch must work (checkpoint inheritance)
	fork2Path, err := c.ForkNamed(0, "branch-from-branch")
	if err != nil {
		t.Fatalf("fork inside fresh branch: %v", err)
	}
	t.Logf("second fork path: %s", fork2Path)

	// Simulate handleDeleteSession: remove only B's .jsonl (ckpt dir stays)
	if err := os.Remove(forkPath); err != nil {
		t.Fatal(err)
	}

	// Return to the source session A (resume)
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}
	c.Resume(loaded, path)

	cps = c.Checkpoints()
	t.Logf("A checkpoints after delete-fork-resume: %+v", cps)
	if len(cps) == 0 {
		t.Fatal("A lost its checkpoints after fork+delete+resume")
	}
	if !c.CheckpointHasBoundary(0) {
		t.Fatal("A boundary 0 missing after resume")
	}

	// Fork again
	if _, err := c.ForkNamed(0, "branch2"); err != nil {
		t.Fatalf("fork after resume: %v", err)
	}
}

// Scenario 2: source runs two turns, fork from turn 1, delete the branch,
// resume the source and fork again.
func TestForkTwoTurnsThenDeleteThenResumeThenForkAgain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-a.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	runner := &recordingSessionRunner{session: sess}
	c := New(Options{
		Runner:      runner,
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
	})

	o := newTurnOrchestrator(c)
	for i := 0; i < 2; i++ {
		prompt := "turn " + string(rune('0'+i))
		if err := o.runTurnWithRawDisplay(context.Background(), prompt, prompt, ""); err != nil {
			t.Fatal(err)
		}
	}
	if !c.CheckpointHasBoundary(0) || !c.CheckpointHasBoundary(1) {
		t.Fatalf("boundaries missing: %+v", c.Checkpoints())
	}

	// Fork from the latest turn (turn 1)
	forkPath, err := c.ForkNamed(1, "branch")
	if err != nil {
		t.Fatalf("fork at turn 1: %v", err)
	}
	t.Logf("forked session path: %s", forkPath)
	if err := os.Remove(forkPath); err != nil {
		t.Fatal(err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("load A: %v", err)
	}
	c.Resume(loaded, path)

	cps := c.Checkpoints()
	t.Logf("A checkpoints after fork+delete+resume: %+v", cps)
	if len(cps) != 2 {
		t.Fatalf("A checkpoints = %+v, want 2 (turns 0,1)", cps)
	}

	// Fork again from the latest turn (turn 1)
	if _, err := c.ForkNamed(1, "branch2"); err != nil {
		t.Fatalf("fork after resume: %v", err)
	}
}

// Reported bug: forking a two-turn session from its latest turn must carry both
// turns into the branch, not collapse it to one turn.
func TestForkFromTwoTurnSessionKeepsBothTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("answer one"),
		textTurn("answer two"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionDir: dir, SessionPath: path, Label: "test"})

	for i := 0; i < 2; i++ {
		prompt := fmt.Sprintf("prompt %d", i+1)
		if err := c.runTurnWithRaw(context.Background(), prompt, prompt); err != nil {
			t.Fatal(err)
		}
	}
	_, srcTurns := agent.SessionPreviewFromMessages(c.History())
	t.Logf("source turns=%d", srcTurns)
	if srcTurns != 2 {
		t.Fatalf("source turns = %d, want 2", srcTurns)
	}
	cps := c.Checkpoints()
	t.Logf("source checkpoints: %+v", cps)

	// Fork from the latest turn (turn 1)
	forkPath, err := c.ForkNamed(1, "branch")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	forked, err := agent.LoadSession(forkPath)
	if err != nil {
		t.Fatalf("load fork: %v", err)
	}
	_, forkTurns := agent.SessionPreviewFromMessages(forked.Messages)
	t.Logf("forked turns=%d msgs=%d", forkTurns, len(forked.Messages))
	if forkTurns != 2 {
		t.Fatalf("forked turns = %d, want 2 (both turns inherited)", forkTurns)
	}
}

// Tip branching (Branch, used by the message-level "fork" button via turn=-1)
// must carry the whole conversation into the branch regardless of turn bookkeeping.
func TestTipBranchFromTwoTurnSessionKeepsBothTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		textTurn("answer one"),
		textTurn("answer two"),
	}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionDir: dir, SessionPath: path, Label: "test"})

	for i := 0; i < 2; i++ {
		prompt := fmt.Sprintf("prompt %d", i+1)
		if err := c.runTurnWithRaw(context.Background(), prompt, prompt); err != nil {
			t.Fatal(err)
		}
	}
	_, srcTurns := agent.SessionPreviewFromMessages(c.History())
	if srcTurns != 2 {
		t.Fatalf("source turns = %d, want 2", srcTurns)
	}

	branchPath, err := c.Branch("tip-branch")
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	branched, err := agent.LoadSession(branchPath)
	if err != nil {
		t.Fatalf("load branch: %v", err)
	}
	_, branchTurns := agent.SessionPreviewFromMessages(branched.Messages)
	t.Logf("tip-branch turns=%d msgs=%d", branchTurns, len(branched.Messages))
	if branchTurns != 2 {
		t.Fatalf("tip branch turns = %d, want 2 (whole conversation inherited)", branchTurns)
	}
	meta, ok, err := agent.LoadBranchMeta(branchPath)
	if err != nil || !ok {
		t.Fatalf("load branch meta ok=%v err=%v", ok, err)
	}
	if meta.ForkTurn != -1 {
		t.Fatalf("tip branch ForkTurn = %d, want -1", meta.ForkTurn)
	}
	// Tip branch must inherit checkpoints too (immediate rewind/fork support)
	ckpt := store.SessionCheckpointDir(branchPath)
	ents, err := os.ReadDir(ckpt)
	if err != nil || len(ents) == 0 {
		t.Fatalf("tip branch checkpoint dir %s: err=%v entries=%d, want inherited", ckpt, err, len(ents))
	}
}
