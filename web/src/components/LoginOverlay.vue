<script setup lang="ts">
import { ref } from "vue"
import { api } from "../api"
const emit = defineEmits<{ (e: "login"): void }>()
const name = ref("")
const error = ref("")
async function doLogin() {
  const n = name.value.trim()
  if (!n) return
  error.value = ""
  try { await api.login(n); emit("login") }
  catch (e: any) { error.value = e.message }
}
function cancel() { emit("login") }
</script>

<template>
  <div class="teamix-login-overlay">
    <div class="teamix-login-box">
      <div class="logo">T</div>
      <h1>Teamix Cloud</h1>
      <p class="sub">输入昵称开始使用</p>
      <div class="error" v-if="error">{{ error }}</div>
      <input type="text" v-model="name" placeholder="你的昵称" autofocus @keydown.enter="doLogin" />
      <div class="btn-row">
        <button class="cancel" @click="cancel">取消</button>
        <button class="join" @click="doLogin">进入</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.teamix-login-overlay { position:fixed;inset:0;background:oklch(0% 0 0/.6);display:flex;align-items:center;justify-content:center;z-index:999;backdrop-filter:blur(4px) }
.teamix-login-box { background:var(--panel);border:1px solid var(--border);border-radius:var(--radius-lg);padding:40px;width:340px;text-align:center;box-shadow:var(--shadow-lg) }
.teamix-login-box .logo { width:48px;height:48px;margin:0 auto 16px;border-radius:14px;background:linear-gradient(135deg,var(--accent),var(--violet));display:flex;align-items:center;justify-content:center;font-size:22px;font-weight:700;color:#000 }
.teamix-login-box h1 { font-family:var(--heading);font-size:22px;font-weight:700;margin-bottom:4px }
.teamix-login-box .sub { color:var(--muted);font-size:14px;margin-bottom:20px }
.teamix-login-box .error { color:var(--danger);font-size:13px;margin-bottom:10px }
.teamix-login-box input { width:100%;padding:10px 14px;border-radius:var(--radius);border:1px solid var(--border);background:var(--bg-2);color:var(--fg);font-size:15px;text-align:center;outline:none;margin-bottom:16px }
.teamix-login-box input:focus { border-color:var(--accent) }
.teamix-login-box .btn-row { display:flex;gap:8px }
.teamix-login-box .cancel,.teamix-login-box .join { flex:1;padding:10px;border-radius:var(--radius);font-size:14px;font-weight:600;border:none;cursor:pointer }
.teamix-login-box .cancel { background:var(--card);color:var(--muted);border:1px solid var(--border) }
.teamix-login-box .join { background:var(--accent);color:#000 }
</style>
