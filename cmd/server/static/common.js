const API = '/api/v1';

function loadSession() {
  return {
    token: localStorage.getItem('token'),
    user: JSON.parse(localStorage.getItem('user') || 'null'),
  };
}

function saveSession(token, user) {
  localStorage.setItem('token', token);
  localStorage.setItem('user', JSON.stringify(user));
}

function clearSession() {
  localStorage.removeItem('token');
  localStorage.removeItem('user');
}

async function api(path, opts = {}) {
  const { token } = loadSession();
  const headers = { 'Content-Type': 'application/json', ...opts.headers };
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(API + path, { ...opts, headers });
  if (res.status === 401) {
    clearSession();
    if (!location.pathname.includes('login')) {
      location.href = '/login.html';
    }
    throw new Error('unauthorized');
  }
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}

function fmtTime(v) {
  return v ? new Date(v).toLocaleString() : '-';
}

function badge(text, ok) {
  return `<span class="badge ${ok ? 'online' : 'offline'}">${text}</span>`;
}
