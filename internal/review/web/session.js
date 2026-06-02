(function () {
  // Pull session id from /review/{id}
  const m = location.pathname.match(/^\/review\/([^\/]+)\/?$/);
  if (!m) {
    document.body.innerHTML = '<p style="padding:24px">Bad session URL.</p>';
    return;
  }
  const sessionID = decodeURIComponent(m[1]);

  const $title = document.getElementById('title');
  const $status = document.getElementById('status-pill');
  const $rev = document.getElementById('revision-meta');
  const $doc = document.getElementById('plan-doc');
  const $comments = document.getElementById('comments');
  const $generalForm = document.getElementById('general-comment-form');
  const $generalBody = document.getElementById('general-comment-body');

  const $anchorDlg = document.getElementById('anchor-dialog');
  const $anchorSnippet = document.getElementById('anchor-snippet');
  const $anchorBody = document.getElementById('anchor-body');
  const $anchorSubmit = document.getElementById('anchor-submit');

  const $requestDlg = document.getElementById('request-dialog');
  const $requestBody = document.getElementById('request-body');
  const $requestSubmit = document.getElementById('request-submit');

  let state = null;
  let pendingAnchor = null;

  // ---- Tiny markdown renderer -------------------------------------------
  // Renders headings, paragraphs, fenced code, inline code, bullet/numbered
  // lists, and blockquotes. Preserves byte offsets so we can map text selections
  // back to plan_markdown.
  //
  // Each rendered block carries data-start / data-end attributes (byte offsets
  // in the source markdown), which lets us derive anchor offsets from DOM
  // ranges.

  // A markdown table separator row: cells of dashes with optional alignment
  // colons, e.g. `| --- | :---: | ---: |`. At least one cell required.
  function isTableSeparator(line) {
    if (!line.includes('|') && !line.includes('-')) return false;
    const cells = splitRow(line);
    return cells.length > 0 && cells.every((c) => /^:?-+:?$/.test(c.trim()));
  }

  // Splits a table row into trimmed cell strings, honoring escaped pipes (\|)
  // and dropping the empty cells produced by leading/trailing pipes.
  function splitRow(line) {
    const cells = line
      .split(/(?<!\\)\|/)            // split on unescaped pipes
      .map((c) => c.replace(/\\\|/g, "|").trim());
    if (cells.length && cells[0] === "") cells.shift();
    if (cells.length && cells[cells.length - 1] === "") cells.pop();
    return cells;
  }

  function renderMarkdown(src) {
    const lines = src.split('\n');
    const tokens = [];
    let i = 0;
    let byteOffset = 0;

    function advance(line) {
      const start = byteOffset;
      const end = byteOffset + line.length;
      byteOffset = end + 1; // +1 for \n
      return [start, end];
    }

    while (i < lines.length) {
      const line = lines[i];
      // Fenced code block
      const fence = line.match(/^```(\w*)\s*$/);
      if (fence) {
        const startOffset = byteOffset;
        advance(line);
        i++;
        const body = [];
        while (i < lines.length && !/^```\s*$/.test(lines[i])) {
          body.push(lines[i]);
          advance(lines[i]);
          i++;
        }
        if (i < lines.length) {
          advance(lines[i]); // closing fence
          i++;
        }
        const endOffset = byteOffset - 1;
        tokens.push({ type: 'code', lang: fence[1], text: body.join('\n'), start: startOffset, end: endOffset });
        continue;
      }
      // Heading
      const heading = line.match(/^(#{1,6})\s+(.+?)\s*$/);
      if (heading) {
        const [start, end] = advance(line);
        tokens.push({ type: 'heading', level: heading[1].length, text: heading[2], start, end });
        i++;
        continue;
      }
      // Blank line
      if (line.trim() === '') {
        advance(line);
        i++;
        continue;
      }
      // Horizontal rule (--- / *** / ___ on its own line). Table separators
      // are consumed by the table branch below, so this is an unambiguous hr.
      if (/^\s*(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
        const [start, end] = advance(line);
        tokens.push({ type: 'hr', start, end });
        i++;
        continue;
      }
      // Table (GitHub-flavored): a header row of `| a | b |` followed by a
      // separator row like `| --- | :--: |`. We capture the whole table as one
      // block (start/end span every consumed line) so selection anchoring maps
      // back to a contiguous source range.
      if (line.includes('|') && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
        const startOff = byteOffset;
        const header = splitRow(line);
        advance(line);
        i++;
        advance(lines[i]); // separator row
        i++;
        const rows = [];
        while (i < lines.length && lines[i].includes('|') && lines[i].trim() !== '') {
          rows.push(splitRow(lines[i]));
          advance(lines[i]);
          i++;
        }
        const endOff = byteOffset - 1;
        tokens.push({ type: 'table', header, rows, start: startOff, end: endOff });
        continue;
      }
      // List item
      const li = line.match(/^(\s*)([-*]|\d+\.)\s+(.+)$/);
      if (li) {
        const ordered = /\d+\./.test(li[2]);
        const items = [];
        while (i < lines.length) {
          const m2 = lines[i].match(/^(\s*)([-*]|\d+\.)\s+(.+)$/);
          if (!m2) break;
          const [s, e] = advance(lines[i]);
          items.push({ text: m2[3], start: s, end: e });
          i++;
        }
        tokens.push({ type: ordered ? 'ol' : 'ul', items });
        continue;
      }
      // Blockquote
      if (line.startsWith('> ')) {
        const buf = [];
        const startOff = byteOffset;
        while (i < lines.length && lines[i].startsWith('> ')) {
          buf.push(lines[i].slice(2));
          advance(lines[i]);
          i++;
        }
        const endOff = byteOffset - 1;
        tokens.push({ type: 'quote', text: buf.join(' '), start: startOff, end: endOff });
        continue;
      }
      // Paragraph: gather contiguous non-blank lines
      const buf = [];
      const startOff = byteOffset;
      while (i < lines.length && lines[i].trim() !== '' && !/^(#{1,6})\s+/.test(lines[i]) && !/^```/.test(lines[i])) {
        buf.push(lines[i]);
        advance(lines[i]);
        i++;
      }
      const endOff = byteOffset - 1;
      tokens.push({ type: 'p', text: buf.join(' '), start: startOff, end: endOff });
    }
    return renderTokens(tokens);
  }

  // Only allow benign URL schemes so a malicious plan can't inject
  // javascript:/data: links. Relative and anchor links are permitted.
  function safeHref(url) {
    const u = url.trim();
    if (/^(https?:|mailto:|tel:)/i.test(u)) return u;
    if (/^[/#?]/.test(u) || /^[\w./-]+$/.test(u)) return u;
    return null;
  }

  function renderInline(text) {
    // Escape, then re-allow inline code, links, and bold/italic/strike.
    const esc = text
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
    return esc
      .replace(/`([^`]+)`/g, '<code>$1</code>')
      .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (m, label, url) => {
        const href = safeHref(url);
        if (!href) return m; // leave unsafe/odd links as literal text
        return `<a href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
      })
      .replace(/~~([^~]+)~~/g, '<del>$1</del>')
      .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
      .replace(/(^|\s)\*([^*\s][^*]*?)\*(?=\s|$|[.,;:!?])/g, '$1<em>$2</em>');
  }

  function renderTokens(tokens) {
    const frag = document.createDocumentFragment();
    for (const t of tokens) {
      let el;
      switch (t.type) {
        case 'heading':
          el = document.createElement('h' + t.level);
          el.innerHTML = renderInline(t.text);
          break;
        case 'p':
          el = document.createElement('p');
          el.innerHTML = renderInline(t.text);
          break;
        case 'code':
          el = document.createElement('pre');
          const code = document.createElement('code');
          code.textContent = t.text;
          el.appendChild(code);
          break;
        case 'quote':
          el = document.createElement('blockquote');
          el.innerHTML = renderInline(t.text);
          break;
        case 'hr':
          el = document.createElement('hr');
          break;
        case 'ul':
        case 'ol':
          el = document.createElement(t.type);
          for (const item of t.items) {
            const li = document.createElement('li');
            // Task list item: `[ ] text` / `[x] text` -> checkbox + label.
            const task = item.text.match(/^\[([ xX])\]\s+(.*)$/);
            if (task) {
              li.classList.add('task');
              const box = document.createElement('input');
              box.type = 'checkbox';
              box.disabled = true;
              box.checked = task[1].toLowerCase() === 'x';
              const span = document.createElement('span');
              span.innerHTML = renderInline(task[2]);
              li.appendChild(box);
              li.appendChild(span);
            } else {
              li.innerHTML = renderInline(item.text);
            }
            li.dataset.start = item.start;
            li.dataset.end = item.end;
            el.appendChild(li);
          }
          break;
        case 'table': {
          el = document.createElement('table');
          const thead = document.createElement('thead');
          const htr = document.createElement('tr');
          for (const cell of t.header) {
            const th = document.createElement('th');
            th.innerHTML = renderInline(cell);
            htr.appendChild(th);
          }
          thead.appendChild(htr);
          el.appendChild(thead);
          const tbody = document.createElement('tbody');
          for (const row of t.rows) {
            const tr = document.createElement('tr');
            // Pad/truncate to the header's column count so ragged rows render.
            for (let c = 0; c < t.header.length; c++) {
              const td = document.createElement('td');
              td.innerHTML = renderInline(row[c] || '');
              tr.appendChild(td);
            }
            tbody.appendChild(tr);
          }
          el.appendChild(tbody);
          break;
        }
      }
      if (el) {
        if (t.start !== undefined) el.dataset.start = t.start;
        if (t.end !== undefined) el.dataset.end = t.end;
        frag.appendChild(el);
      }
    }
    return frag;
  }

  // ---- Selection-based commenting ---------------------------------------
  //
  // The agent's plan is byte-indexed on the server (Go uses byte offsets). The
  // DOM gives us UTF-16 code units. We compute the prefix string up to the
  // selection, then `TextEncoder().encode(...).length` to get the UTF-8 byte
  // count, so anchors round-trip correctly for plans with emoji / non-ASCII.

  const utf8 = new TextEncoder();

  function findOffsetsFromSelection() {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed) return null;
    const range = sel.getRangeAt(0);

    const startBlock = ancestorWith(range.startContainer, 'start');
    const endBlock = ancestorWith(range.endContainer, 'end');
    if (!startBlock || !endBlock) return null;

    const blockStart = parseInt(startBlock.dataset.start, 10);
    if (Number.isNaN(blockStart)) return null;

    const startByteInBlock = byteOffsetWithin(startBlock, range.startContainer, range.startOffset);
    let endAbs;
    if (startBlock === endBlock) {
      const endByteInBlock = byteOffsetWithin(endBlock, range.endContainer, range.endOffset);
      endAbs = blockStart + endByteInBlock;
    } else {
      const endBlockStart = parseInt(endBlock.dataset.start, 10);
      if (Number.isNaN(endBlockStart)) return null;
      const endByteInBlock = byteOffsetWithin(endBlock, range.endContainer, range.endOffset);
      endAbs = endBlockStart + endByteInBlock;
    }

    return {
      start: blockStart + startByteInBlock,
      end: endAbs,
      sectionId: nearestSectionID(startBlock),
      snippet: sel.toString(),
    };
  }

  function ancestorWith(node, attr) {
    let n = node;
    while (n && n !== document) {
      if (n.dataset && n.dataset[attr] !== undefined) return n;
      n = n.parentNode;
    }
    return null;
  }

  // Walks the block's text nodes in order, accumulating the prefix text up to
  // (node, offset). Returns the UTF-8 byte length of that prefix.
  function byteOffsetWithin(block, node, offset) {
    let prefix = '';
    const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const n = walker.currentNode;
      if (n === node) {
        prefix += n.nodeValue.slice(0, offset);
        return utf8.encode(prefix).length;
      }
      prefix += n.nodeValue;
    }
    return utf8.encode(prefix).length;
  }

  function nearestSectionID(block) {
    if (!state || !state.current) return '';
    const start = parseInt(block.dataset.start, 10);
    let best = '';
    let bestLevel = -1;
    for (const s of state.current.sections || []) {
      if (start >= s.start_offset && start < s.end_offset && s.level > bestLevel) {
        best = s.id;
        bestLevel = s.level;
      }
    }
    return best;
  }

  document.addEventListener('mouseup', () => {
    const anchor = findOffsetsFromSelection();
    if (!anchor) return;
    if (anchor.snippet.trim().length < 2) return;
    pendingAnchor = anchor;
    $anchorSnippet.textContent = anchor.snippet.length > 240 ? anchor.snippet.slice(0, 240) + '…' : anchor.snippet;
    $anchorBody.value = '';
    $anchorDlg.showModal();
    requestAnimationFrame(() => $anchorBody.focus());
  });

  $anchorSubmit.addEventListener('click', async (e) => {
    if (!pendingAnchor) return;
    const body = $anchorBody.value.trim();
    if (!body) {
      e.preventDefault();
      return;
    }
    await postComment({
      body,
      anchor_section_id: pendingAnchor.sectionId,
      anchor_start_offset: pendingAnchor.start,
      anchor_end_offset: pendingAnchor.end,
    });
    pendingAnchor = null;
  });

  $generalForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const body = $generalBody.value.trim();
    if (!body) return;
    await postComment({ body });
    $generalBody.value = '';
  });

  async function postComment(payload) {
    const res = await fetch(`/api/review/${encodeURIComponent(sessionID)}/comments`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
    if (!res.ok) {
      alert('Failed: ' + (await res.text()));
      return;
    }
    await load();
  }

  // ---- Approve / cancel / request --------------------------------------
  document.getElementById('btn-approve').addEventListener('click', async () => {
    if (!confirm('Approve this plan? The agent will start building.')) return;
    await fetch(`/api/review/${encodeURIComponent(sessionID)}/approve`, { method: 'POST' });
  });
  document.getElementById('btn-cancel').addEventListener('click', async () => {
    if (!confirm('Cancel this review?')) return;
    await fetch(`/api/review/${encodeURIComponent(sessionID)}/cancel`, { method: 'POST' });
  });
  document.getElementById('btn-request').addEventListener('click', () => {
    $requestBody.value = '';
    $requestDlg.showModal();
    requestAnimationFrame(() => $requestBody.focus());
  });
  $requestSubmit.addEventListener('click', async (e) => {
    const body = $requestBody.value.trim();
    if (!body) { e.preventDefault(); return; }
    await fetch(`/api/review/${encodeURIComponent(sessionID)}/request-revision`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ body }),
    });
  });

  // ---- Render ----------------------------------------------------------
  function renderState() {
    if (!state) return;
    $title.textContent = state.session.title || sessionID;
    $status.className = `pill ${state.session.status}`;
    $status.textContent = state.session.status.replace(/_/g, ' ');
    if (state.current) {
      $rev.textContent = `revision ${state.current.revision_number}`;
    }

    $doc.innerHTML = '';
    if (state.current) {
      $doc.appendChild(renderMarkdown(state.current.plan_markdown));
    }

    renderComments();
  }

  function renderComments() {
    $comments.innerHTML = '';
    // Group by thread root (parent_id == "" is a root).
    const byParent = new Map();
    const roots = [];
    for (const c of state.comments) {
      if (!c.parent_id) {
        roots.push(c);
      } else {
        if (!byParent.has(c.parent_id)) byParent.set(c.parent_id, []);
        byParent.get(c.parent_id).push(c);
      }
    }
    if (!roots.length) {
      const empty = document.createElement('p');
      empty.className = 'empty';
      empty.textContent = 'No comments yet. Select text in the plan to leave one.';
      $comments.appendChild(empty);
      return;
    }
    for (const root of roots) {
      const wrap = document.createElement('div');
      wrap.className = 'comment-thread' + (root.status === 'orphaned' ? ' orphan' : '');
      const anchor = document.createElement('div');
      anchor.className = 'anchor';
      if (root.status === 'orphaned') {
        anchor.textContent = '⚠ anchor lost in revision';
      } else if (root.anchor_section_id) {
        anchor.textContent = `↳ §${root.anchor_section_id}`;
      } else {
        anchor.textContent = '↳ general';
      }
      wrap.appendChild(anchor);
      wrap.appendChild(renderComment(root));
      const replies = byParent.get(root.id) || [];
      for (const r of replies) wrap.appendChild(renderComment(r));

      const replyForm = document.createElement('div');
      replyForm.className = 'thread-reply';
      const input = document.createElement('input');
      input.placeholder = 'Reply to this thread…';
      const btn = document.createElement('button');
      btn.textContent = 'Reply';
      btn.addEventListener('click', async () => {
        const body = input.value.trim();
        if (!body) return;
        await postComment({ body, parent_comment_id: root.id });
        input.value = '';
      });
      replyForm.appendChild(input);
      replyForm.appendChild(btn);
      wrap.appendChild(replyForm);

      $comments.appendChild(wrap);
    }
  }

  function renderComment(c) {
    const el = document.createElement('div');
    el.className = 'comment';
    const who = document.createElement('span');
    who.className = 'who ' + c.author;
    who.textContent = c.author === 'agent' ? 'Agent' : 'You';
    const when = document.createElement('span');
    when.className = 'when';
    when.textContent = relTime(c.created_at);
    const body = document.createElement('div');
    body.className = 'body';
    body.textContent = c.body;
    el.appendChild(who);
    el.appendChild(when);
    el.appendChild(body);
    return el;
  }

  function relTime(iso) {
    const t = new Date(iso);
    if (Number.isNaN(t.getTime())) return iso;
    const diff = (Date.now() - t.getTime()) / 1000;
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return t.toLocaleString();
  }

  // ---- Load + live updates ---------------------------------------------
  async function load() {
    const res = await fetch(`/api/review/${encodeURIComponent(sessionID)}/state`);
    if (!res.ok) {
      $doc.innerHTML = `<p>Failed to load session.</p>`;
      return;
    }
    state = await res.json();
    renderState();
  }

  function subscribeEvents() {
    const es = new EventSource(`/api/review/${encodeURIComponent(sessionID)}/events`);
    es.addEventListener('comment_added', load);
    es.addEventListener('reply_added', load);
    es.addEventListener('revision_submitted', load);
    es.addEventListener('approved', load);
    es.addEventListener('cancelled', load);
    es.addEventListener('revision_requested', load);
    es.onerror = () => { /* browser will reconnect */ };
  }

  load().then(subscribeEvents);
})();
