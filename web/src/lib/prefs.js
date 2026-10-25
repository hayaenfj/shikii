// Reading and writing localStorage. Where it is not available (private mode and so on), it is silently ignored (the page keeps working)
export function read(key) {
  try { return localStorage.getItem(key); } catch (e) { return null; }
}

export function write(key, value) {
  try { localStorage.setItem(key, value); } catch (e) { /* the page still works even if it cannot save */ }
}
