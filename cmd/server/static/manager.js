const { user } = loadSession();
if (!user) location.href = '/login.html';

let term = null, ws = null;
const tenantMap = {};

document.getElementById('user-info').textContent =
  `${user.username} (${user.role})${user.tenant_slug ? ' · ' + user.tenant_slug : ''}`;

if (user.role === 'root') {
  document.querySelectorAll('.root-only').forEach(el => el.classList.remove('hidden'));
}
if (user.role === 'root' || user.role === 'admin') {
  document.querySelectorAll('.admin-only').forEach(el => el.classList.remove('hidden'));
}

function showPage(name) {
  document.querySelectorAll('.page').forEach(p => p.classList.add('hidden'));
  document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
  document.getElementById(name + '-page')?.classList.remove('hidden');
  document.querySelector(`[data-page="${name}"]`)?.classList.add('active');
}

function logout() {
  clearSession();
  location.href = '/login.html';
}

async function loadTenantsMap() {
  if (user.role !== 'root') return;
  const tenants = await api('/tenants');
  tenants.forEach(t => { tenantMap[t.id] = t.slug; });
}

function tenantLabel(id) {
  return tenantMap[id] || id?.slice(0, 8) || '-';
}

async function loadDashboard() {
  const [hosts, agents, sessions, audit] = await Promise.all([
    api('/hosts'), api('/agents'), api('/sessions/active'), api('/audit?limit=100'),
  ]);
  document.getElementById('stat-hosts').textContent = hosts.length;
  document.getElementById('stat-agents').textContent = agents.filter(a => a.status === 'online').length;
  document.getElementById('stat-sessions').textContent = sessions.length;
  const today = new Date().toISOString().slice(0, 10);
  document.getElementById('stat-audit').textContent = audit.filter(a => a.created_at?.startsWith(today)).length;
}

async function loadHosts() {
  const hosts = await api('/hosts');
  document.getElementById('hosts-table').innerHTML = hosts.map(h => `
    <tr>
      <td>${h.name}</td><td>${h.address}</td><td>${h.port}</td><td>${h.username}</td>
      <td>${tenantLabel(h.tenant_id)}</td>
      <td>${badge(h.enabled ? '启用' : '禁用', h.enabled)}</td>
      <td>
        <button class="btn-secondary" onclick="editHost('${h.id}')">编辑</button>
        <button class="btn-danger" onclick="deleteHost('${h.id}')">删除</button>
      </td>
    </tr>`).join('');
  document.getElementById('webssh-host').innerHTML = hosts.filter(h => h.enabled)
    .map(h => `<option value="${h.id}">${h.name} (${h.address})</option>`).join('');
}

async function loadAgents() {
  const agents = await api('/agents');
  document.getElementById('agents-table').innerHTML = agents.map(a => `
    <tr>
      <td>${a.name}</td><td>${tenantLabel(a.tenant_id)}</td><td>${a.ip || '-'}</td>
      <td>${badge(a.status, a.status === 'online')}</td><td>${fmtTime(a.last_seen)}</td>
    </tr>`).join('');
  document.getElementById('agent-doc').textContent =
`curl -X POST ${location.origin}/api/v1/agents/register -H "Content-Type: application/json" -d '{
  "name":"node-1","register_token":"YOUR_TOKEN","tenant_slug":"default",
  "hostname":"n1","ip":"10.0.0.1","version":"1.0"
}'`;
}

async function loadSessions() {
  const rows = await api('/sessions?limit=50');
  document.getElementById('sessions-table').innerHTML = rows.map(s => `
    <tr><td>${s.username}</td><td>${s.host_name || s.target_addr}</td><td>${s.protocol}</td>
    <td>${badge(s.status, s.status === 'active')}</td><td>${fmtTime(s.started_at)}</td></tr>`).join('');
}

async function loadAudit() {
  const rows = await api('/audit?limit=100');
  document.getElementById('audit-table').innerHTML = rows.map(l => `
    <tr><td>${fmtTime(l.created_at)}</td><td>${l.username}</td><td>${l.action}</td><td>${l.detail}</td></tr>`).join('');
}

async function loadMaintenance() {
  const stats = await api('/audit/stats');
  document.getElementById('audit-total').textContent = stats.total ?? 0;
  document.getElementById('audit-oldest').textContent = stats.oldest ? fmtTime(stats.oldest) : '-';
  document.getElementById('audit-newest').textContent = stats.newest ? fmtTime(stats.newest) : '-';
}

function showCleanupResult(msg) {
  const el = document.getElementById('cleanup-result');
  el.textContent = msg;
  el.classList.remove('hidden');
}

async function cleanupAudit(query) {
  if (!confirm('确认执行清理？此操作不可撤销。')) return;
  const res = await api('/audit?' + query, { method: 'DELETE' });
  showCleanupResult(`已删除 ${res.deleted} 条审计记录`);
  await loadMaintenance();
  await loadAudit();
}

document.getElementById('cleanup-days-btn')?.addEventListener('click', () => {
  const days = document.getElementById('cleanup-days').value;
  if (!days || days < 1) return alert('请输入有效天数');
  cleanupAudit('older_than_days=' + encodeURIComponent(days));
});
document.getElementById('cleanup-before-btn')?.addEventListener('click', () => {
  const val = document.getElementById('cleanup-before').value;
  if (!val) return alert('请选择日期');
  cleanupAudit('before=' + encodeURIComponent(new Date(val).toISOString()));
});
document.getElementById('cleanup-all-btn')?.addEventListener('click', () => cleanupAudit('all=true'));
document.getElementById('audit-refresh-btn')?.addEventListener('click', loadAudit);

async function loadUsers() {
  const rows = await api('/users');
  document.getElementById('users-table').innerHTML = rows.map(u => `
    <tr><td>${u.username}</td><td>${u.role}</td><td>${tenantLabel(u.tenant_id)}</td>
    <td>${badge(u.enabled ? '启用' : '禁用', u.enabled)}</td></tr>`).join('');
}

async function loadTenants() {
  const rows = await api('/tenants');
  rows.forEach(t => { tenantMap[t.id] = t.slug; });
  document.getElementById('tenants-table').innerHTML = rows.map(t => `
    <tr><td>${t.name}</td><td>${t.slug}</td><td>${badge(t.enabled ? '启用' : '禁用', t.enabled)}</td>
    <td>${fmtTime(t.created_at)}</td></tr>`).join('');
}

function openModal(title, body, onSave) {
  document.getElementById('modal-title').textContent = title;
  document.getElementById('modal-body').innerHTML = body;
  const form = document.getElementById('modal-form');
  form.onsubmit = async (e) => {
    e.preventDefault();
    await onSave(new FormData(form));
    document.getElementById('modal').close();
  };
  document.getElementById('modal').showModal();
}

window.editHost = async (id) => {
  const h = await api('/hosts/' + id);
  openModal('编辑主机', `
    <label>名称</label><input name="name" value="${h.name}" required>
    <label>地址</label><input name="address" value="${h.address}" required>
    <label>端口</label><input name="port" type="number" value="${h.port}">
    <label>SSH 用户</label><input name="username" value="${h.username}">`,
    async (fd) => {
      const data = Object.fromEntries(fd);
      data.port = parseInt(data.port) || 22;
      data.enabled = true;
      await api('/hosts/' + id, { method: 'PUT', body: JSON.stringify({ ...h, ...data }) });
      loadHosts();
    });
};

window.deleteHost = async (id) => {
  if (!confirm('确认删除？')) return;
  await api('/hosts/' + id, { method: 'DELETE' });
  loadHosts();
};

function connectWebSSH() {
  const hostId = document.getElementById('webssh-host').value;
  const password = document.getElementById('webssh-pass').value;
  if (!hostId) return;
  if (ws) { ws.close(); ws = null; }
  if (term) { term.dispose(); term = null; }
  term = new Terminal({ cursorBlink: true, theme: { background: '#000' } });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('terminal'));
  fit.fit();
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  ws = new WebSocket(`${proto}//${location.host}/api/v1/webssh?token=${loadSession().token}`);
  ws.onopen = () => ws.send(JSON.stringify({ type: 'connect', host_id: hostId, password: password || undefined, cols: term.cols, rows: term.rows }));
  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.type === 'data') term.write(msg.data);
    else if (msg.type === 'error') term.writeln('\r\n\x1b[31m' + msg.message);
    else if (msg.type === 'connected') term.writeln('\r\n\x1b[32m已连接\x1b[0m\r\n');
  };
  term.onData(d => ws?.readyState === 1 && ws.send(JSON.stringify({ type: 'data', data: d })));
}

document.getElementById('logout-btn').onclick = logout;
document.getElementById('add-host-btn').onclick = () => openModal('添加主机', `
  <label>名称</label><input name="name" required>
  <label>地址</label><input name="address" required>
  <label>端口</label><input name="port" type="number" value="22">
  <label>SSH 用户</label><input name="username" value="root">`,
  async (fd) => {
    const data = Object.fromEntries(fd);
    data.port = parseInt(data.port) || 22;
    data.enabled = true;
    await api('/hosts', { method: 'POST', body: JSON.stringify(data) });
    loadHosts();
  });

document.getElementById('add-user-btn').onclick = () => openModal('添加用户', `
  <label>用户名</label><input name="username" required>
  <label>密码</label><input name="password" type="password" required>
  <label>角色</label><select name="role"><option value="operator">operator</option><option value="admin">admin</option></select>`,
  async (fd) => {
    await api('/users', { method: 'POST', body: JSON.stringify(Object.fromEntries(fd)) });
    loadUsers();
  });

document.getElementById('add-tenant-btn')?.addEventListener('click', () => openModal('添加租户', `
  <label>名称</label><input name="name" required>
  <label>Slug</label><input name="slug" required>`,
  async (fd) => {
    const data = Object.fromEntries(fd);
    data.enabled = true;
    await api('/tenants', { method: 'POST', body: JSON.stringify(data) });
    loadTenants();
  }));

document.getElementById('webssh-connect').onclick = connectWebSSH;
document.getElementById('webssh-disconnect').onclick = () => { ws?.close(); term?.dispose(); };
document.getElementById('modal-cancel').onclick = () => document.getElementById('modal').close();

document.querySelectorAll('.nav-item').forEach(btn => {
  btn.onclick = () => {
    const page = btn.dataset.page;
    showPage(page);
    if (page === 'dashboard') loadDashboard();
    if (page === 'hosts') loadHosts();
    if (page === 'agents') loadAgents();
    if (page === 'sessions') loadSessions();
    if (page === 'audit') loadAudit();
    if (page === 'maintenance') loadMaintenance();
    if (page === 'users') loadUsers();
    if (page === 'tenants') loadTenants();
    if (page === 'webssh') loadHosts();
  };
});

(async () => {
  await loadTenantsMap();
  showPage('dashboard');
  loadDashboard();
})();
