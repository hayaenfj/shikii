// Text shown as plain text (0.9.2). Agents write Markdown; the page never renders HTML, so the marks are removed instead:
// bold, heading marks, code fences and backquotes go away, and list marks at the start of a line become a middle dot (U+30FB)
export function plainText(v) {
  if (typeof v !== 'string') return v;
  return v
    .replace(/^\s*```.*$/gm, '')
    .replace(/\*\*(.+?)\*\*/g, '$1')
    .replace(/^#{1,6} +/gm, '')
    .replace(/^(\s*)[-*+] +/gm, '$1\u30FB')
    .replace(/`([^`\n]+)`/g, '$1');
}
