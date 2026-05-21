(function () {
  const list = document.getElementById('session-list');
  const filters = {
    awaiting_review: document.getElementById('filter-open'),
    revising: document.getElementById('filter-revising'),
    approved: document.getElementById('filter-approved'),
    cancelled: document.getElementById('filter-cancelled'),
  };

  function selectedStatuses() {
    return Object.entries(filters)
      .filter(([_, el]) => el.checked)
      .map(([k]) => k);
  }

  async function load() {
    const params = new URLSearchParams();
    selectedStatuses().forEach(s => params.append('status', s));
    const url = '/api/review/sessions' + (params.toString() ? `?${params}` : '');
    const res = await fetch(url);
    if (!res.ok) {
      list.innerHTML = `<li class="empty">Failed to load: ${res.status}</li>`;
      return;
    }
    const data = await res.json();
    render(data.sessions || []);
  }

  function render(sessions) {
    if (!sessions.length) {
      list.innerHTML = `<li class="empty">No plan reviews match these filters.</li>`;
      return;
    }
    list.innerHTML = '';
    for (const s of sessions) {
      const li = document.createElement('li');
      const title = document.createElement('div');
      title.className = 'title';
      const a = document.createElement('a');
      a.href = `/review/${encodeURIComponent(s.id)}`;
      a.textContent = s.title || s.id;
      title.appendChild(a);

      const sub = document.createElement('div');
      sub.className = 'sub';
      sub.textContent = `${s.id} · ${s.work_dir || ''} · updated ${formatTime(s.updated_at)}`;

      const pill = document.createElement('span');
      pill.className = `pill ${s.status}`;
      pill.textContent = s.status.replace(/_/g, ' ');

      li.appendChild(title);
      li.appendChild(pill);
      li.appendChild(sub);
      list.appendChild(li);
    }
  }

  function formatTime(iso) {
    if (!iso) return '';
    const t = new Date(iso);
    if (Number.isNaN(t.getTime())) return iso;
    const diff = (Date.now() - t.getTime()) / 1000;
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return t.toLocaleString();
  }

  Object.values(filters).forEach(el => el.addEventListener('change', load));
  load();
  setInterval(load, 5000);
})();
