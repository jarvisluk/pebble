import assert from 'node:assert/strict';
import test from 'node:test';
import { renderMarkdownHtml } from './markdown.ts';

test('renderMarkdownHtml covers full Markdown/GFM and edge cases', () => {
  const input = [
    '# H1',
    '###### H6',
    '',
    '普通段落包含 **粗体**、*斜体*、***粗斜体***、~~删除线~~、__双下划线__ 和 _单下划线_。',
    '',
    '保护规则：snake_case_name 不变，$100 - $200 不变，行内代码 `**not bold** https://not.link` 不被破坏。',
    '',
    '行内公式 $E = mc^2$，脚注引用[^1]，链接 [文档](https://example.com/a(b)) 和自动链接 <mailto:test@example.com> 与裸链接 https://example.com/foo。',
    '',
    '| 列A | 列B | 列C |',
    '| :--- | :---: | ---: |',
    '| **加粗** | `code\\|pipe` | 第一行<br>第二行 |',
    '',
    '- [x] 已完成任务',
    '  - [ ] 缩进未完成任务',
    '1. 第一项',
    '3. 第三项',
    '',
    '> 第一行引用',
    '> > 嵌套引用',
    '',
    '___',
    '',
    '$$',
    '\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2} + \\sqrt{a^2 + b^2}',
    '$$',
    '',
    '<details><summary>展开细节</summary>包含 **加粗** 与 <kbd>⌘K</kbd> 和 H<sub>2</sub>O</details>',
    '',
    '~~~ts',
    'const a = "<script>alert(1)</script>";',
    '~~~',
    '',
    '![外链图](https://example.com/img.png) ![本地图](chart.png)',
    '',
    '[xss](javascript:alert(1)) <script>alert(1)</script> <u onclick="alert(1)">bad</u> <img src=x onerror=alert(1)>',
    '',
    '[^1]: 脚注说明',
  ].join('\n');

  const html = renderMarkdownHtml(input, '复制');

  assert.match(html, /<h1>H1<\/h1>/);
  assert.match(html, /<h6>H6<\/h6>/);
  assert.match(html, /<strong>粗体<\/strong>/);
  assert.match(html, /<em>斜体<\/em>/);
  assert.match(html, /<strong><em>粗斜体<\/em><\/strong>/);
  assert.match(html, /<del>删除线<\/del>/);
  assert.match(html, /<strong>双下划线<\/strong>/);
  assert.match(html, /<em>单下划线<\/em>/);

  // snake_case & currency & inline code protection
  assert.ok(html.includes('snake_case_name'), 'snake_case should stay intact');
  assert.ok(html.includes('$100 - $200'), 'currency should not become math');
  assert.ok(html.includes('<code>**not bold** https://not.link</code>'), 'inline code should not be parsed');

  // math, footnote, links
  assert.ok(html.includes('<span class="md-math-inline">E = mc<sup>2</sup></span>'));
  assert.ok(html.includes('<sup class="md-fn-ref">1</sup>'));
  assert.ok(html.includes('<a href="https://example.com/a(b)"'));
  assert.ok(html.includes('<a href="mailto:test@example.com"'));
  assert.ok(html.includes('<a href="https://example.com/foo"'));

  // GFM table
  assert.ok(html.includes('<div class="md-table-wrap"><table>'));
  assert.ok(html.includes('<th style="text-align:center">列B</th>'));
  assert.ok(html.includes('<td style="text-align:right">第一行<br>第二行</td>'));

  // Task list & indentation & ordered list
  assert.ok(html.includes('md-task is-checked'));
  assert.ok(html.includes('md-indent-1 md-task'));
  assert.ok(html.includes('<span class="md-li-mark">3.</span>'));

  // Blockquote nesting & hr & block math & details & tilde code fence
  assert.ok(html.includes('<blockquote><p>第一行引用</p><blockquote><p>嵌套引用</p></blockquote></blockquote>'));
  assert.ok(html.includes('<hr>'));
  assert.ok(html.includes('∑<sub>i=1</sub><sup>n</sup>'));
  assert.ok(html.includes('class="md-frac"'));
  assert.ok(html.includes('class="md-sqrt"'));
  assert.ok(html.includes('<details class="md-details"><summary>展开细节</summary>'));
  assert.ok(html.includes('<kbd>⌘K</kbd>'));
  assert.ok(html.includes('H<sub>2</sub>O'));
  assert.ok(html.includes('<span>ts</span>') && html.includes('&lt;script&gt;alert(1)&lt;/script&gt;'));

  // Images & XSS
  assert.ok(html.includes('<img class="md-img" src="https://example.com/img.png" alt="外链图"'));
  assert.ok(html.includes('<span class="md-img-fallback">[图片: 本地图]</span>'));
  assert.ok(!html.includes('href="javascript:'));
  assert.ok(!html.includes('<script>alert'));
  assert.ok(html.includes('&lt;u onclick=&quot;alert(1)&quot;&gt;bad&lt;/u&gt;'));
  assert.ok(html.includes('&lt;img src=x onerror=alert(1)&gt;'));
  assert.ok(html.includes('<div class="md-footnote"><span class="md-fn-id">1</span>'));
});
