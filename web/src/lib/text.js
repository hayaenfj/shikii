// Text shown as plain text (0.9.2). Agents write Markdown; the page never renders HTML, so the marks are removed instead:
// bold, heading marks, code fences and backquotes go away, and list marks at the start of a line become "・"
export function plainText(v) {
  if (typeof v !== 'string') return v;
  return v
    .replace(/^\s*```.*$/gm, '')
    .replace(/\*\*(.+?)\*\*/g, '$1')
    .replace(/^#{1,6} +/gm, '')
    .replace(/^(\s*)[-*+] +/gm, '$1・')
    .replace(/`([^`\n]+)`/g, '$1');
}
