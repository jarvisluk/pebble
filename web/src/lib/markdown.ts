export function escHtml(s: unknown): string {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }[c] || c));
}

function inlineMd(s: string): string {
  return s
    .replace(/`([^`\n]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
    .replace(
      /\[([^\]\n]+)\]\((https?:\/\/[^)\s]+)\)/g,
      '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>'
    )
    .replace(
      /(^|[\s(])(https?:\/\/[^\s<)]+)/g,
      '$1<a href="$2" target="_blank" rel="noopener noreferrer">$2</a>'
    );
}

export function renderMarkdownHtml(text: string, copyLabel: string): string {
  const out: string[] = [];
  escHtml(text || '')
    .split(/```/)
    .forEach((part, i) => {
      if (i % 2) {
        const nl = part.indexOf('\n');
        const lang = nl > -1 ? part.slice(0, nl).trim() : '';
        const code = (nl > -1 ? part.slice(nl + 1) : part).replace(/\n$/, '');
        out.push(
          `<div class="code"><div class="code-h"><span>${lang || 'code'}</span><button type="button" data-copy>${escHtml(copyLabel)}</button></div><pre><code>${code}</code></pre></div>`
        );
        return;
      }
      let list: { t: 'ul' | 'ol'; items: string[] } | null = null;
      let para: string[] = [];
      const fp = () => {
        if (para.length) {
          out.push(`<p>${para.map(inlineMd).join('<br>')}</p>`);
          para = [];
        }
      };
      const fl = () => {
        if (list) {
          out.push(`<${list.t}>${list.items.map((x) => `<li>${inlineMd(x)}</li>`).join('')}</${list.t}>`);
          list = null;
        }
      };
      for (const line of part.split('\n')) {
        let m: RegExpMatchArray | null;
        if (!line.trim()) {
          fp();
          fl();
          continue;
        }
        if ((m = line.match(/^(#{1,4})\s+(.*)$/))) {
          fp();
          fl();
          out.push(`<h${m[1].length}>${inlineMd(m[2])}</h${m[1].length}>`);
          continue;
        }
        if (/^\s*(-{3,}|\*{3,})\s*$/.test(line)) {
          fp();
          fl();
          out.push('<hr>');
          continue;
        }
        if ((m = line.match(/^&gt;\s?(.*)$/))) {
          fp();
          fl();
          out.push(`<blockquote>${inlineMd(m[1])}</blockquote>`);
          continue;
        }
        if ((m = line.match(/^\s*[-*+]\s+(.*)$/)) || (m = line.match(/^\s*\d+[.)]\s+(.*)$/))) {
          const listTag: 'ul' | 'ol' = /^\s*\d/.test(line) ? 'ol' : 'ul';
          fp();
          if (!list || list.t !== listTag) {
            fl();
            list = { t: listTag, items: [] };
          }
          list.items.push(m[1]);
          continue;
        }
        fl();
        para.push(line);
      }
      fp();
      fl();
    });
  return `<div class="md">${out.join('')}</div>`;
}
