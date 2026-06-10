document.getElementById('login-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const username = document.getElementById('login-user').value.trim();
  const password = document.getElementById('login-pass').value;
  const tenant_slug = document.getElementById('tenant-slug').value.trim();
  const errEl = document.getElementById('login-error');
  errEl.textContent = '';
  try {
    const body = { username, password };
    if (tenant_slug) body.tenant_slug = tenant_slug;
    const res = await fetch('/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) {
      errEl.textContent = '登录失败，请检查租户、用户名和密码';
      return;
    }
    const data = await res.json();
    saveSession(data.token, data);
    location.href = '/manager.html';
  } catch {
    errEl.textContent = '无法连接服务器';
  }
});

if (loadSession().token) {
  location.href = '/manager.html';
}
