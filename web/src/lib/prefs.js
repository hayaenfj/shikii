// localStorage の読み書き。使えない環境(プライベートモードなど)では黙って無視する(画面は動き続ける)
export function read(key) {
  try { return localStorage.getItem(key); } catch (e) { return null; }
}

export function write(key, value) {
  try { localStorage.setItem(key, value); } catch (e) { /* 保存できなくても画面は使える */ }
}
