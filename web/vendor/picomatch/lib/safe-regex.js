'use strict';

/**
 * shikii patch for GHSA-fc5q-39xc-vjpf (see ../SHIKII-PATCH.md).
 *
 * A repeated group whose body can be split into pieces in many ways, such as `(?:a|ab|b)+`, `(a|[^/]*?b)+` or
 * `(?:a[^/]*?)+`, makes a backtracking engine try every split of a non-matching input: the time grows exponentially
 * with its length. This module finds such repeats in a finished regular expression source and replaces each with
 * an equal expression in which every text has exactly one way to match, so the time stays linear.
 *
 * How: the repeat is read into a tree (only plain regular syntax: literals, classes, groups, alternation, `* + ?`),
 * turned into a position automaton, and tested for exponential ambiguity (two different paths on the same word that
 * start and end in the same state, found as a cycle through both a diagonal and an off-diagonal pair of states in the
 * automaton's square). When it is ambiguous, the automaton is made deterministic (subset construction), minimized and
 * written back as an expression by state elimination. A deterministic automaton gives each text one path, and the
 * expression read off it keeps that property, so it matches exactly the same texts.
 *
 * A repeat that holds a lookahead, an anchor, a back reference or a counted repeat is not analysed (a lookahead can see past
 * the end of the repeat, so no equal expression can be built from the repeat alone), and neither is anything under a `u`,
 * `v`, `m` or `s` flag, a letter with case folding outside ASCII, or an automaton over the size limits: it is left as it was.
 */

const MAX = 0xffff;
const LIMIT = { source: 20000, positions: 160, cells: 200, states: 256, output: 65536, work: 3000000 };

class Unknown extends Error {}

const unknown = () => {
  throw new Unknown('unknown');
};

/* ---------- sets of UTF-16 code units, as sorted [low, high] pairs ---------- */

const normalize = ranges => {
  const sorted = ranges.slice().sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  const out = [];

  for (const [lo, hi] of sorted) {
    const last = out[out.length - 1];

    if (last && lo <= last[1] + 1) {
      if (hi > last[1]) last[1] = hi;
    } else {
      out.push([lo, hi]);
    }
  }

  return out;
};

const complement = ranges => {
  const out = [];
  let next = 0;

  for (const [lo, hi] of ranges) {
    if (lo > next) out.push([next, lo - 1]);
    next = hi + 1;
  }

  if (next <= MAX) out.push([next, MAX]);
  return out;
};

const size = ranges => ranges.reduce((sum, [lo, hi]) => sum + hi - lo + 1, 0);

// Case-insensitive matching (the `i` flag, no `u`): ASCII letters pair up, and a letter outside ASCII could pair with
// another one, so a set that holds one is not handled.
let casedPrefix;

const casedOutsideAscii = ranges => {
  if (!casedPrefix) {
    casedPrefix = new Uint32Array(MAX + 2);

    for (let c = 0; c <= MAX; c++) {
      const ch = String.fromCharCode(c);
      casedPrefix[c + 1] = casedPrefix[c] + (c >= 128 && ch.toLowerCase() !== ch.toUpperCase() ? 1 : 0);
    }
  }

  return ranges.some(([lo, hi]) => hi >= 128 && casedPrefix[hi + 1] - casedPrefix[Math.max(lo, 128)] > 0);
};

const closeCase = ranges => {
  if (casedOutsideAscii(ranges)) unknown();
  const extra = [];

  for (const [lo, hi] of ranges) {
    const lower = [Math.max(lo, 97), Math.min(hi, 122)];
    const upper = [Math.max(lo, 65), Math.min(hi, 90)];
    if (lower[0] <= lower[1]) extra.push([lower[0] - 32, lower[1] - 32]);
    if (upper[0] <= upper[1]) extra.push([upper[0] + 32, upper[1] + 32]);
  }

  return normalize([...ranges, ...extra]);
};

/* ---------- reading the source into a tree ---------- */

const isAlnum = ch => /[A-Za-z0-9]/.test(ch);

// Node types: set, cat, alt, group, rep (all "pure": plain regular syntax), look, count and opaque (read, not analysed).
const parseSource = (src, icase) => {
  let i = 0;

  const makeSet = (ranges, negate) => {
    let set = normalize(ranges);
    if (icase) set = closeCase(set);
    if (negate) set = complement(set);
    return { type: 'set', set, pure: true };
  };

  const parseClass = () => {
    i++;
    const negate = src[i] === '^';
    if (negate) i++;
    const ranges = [];
    let opaque = false;

    const readOne = () => {
      const ch = src[i];
      if (ch === undefined) unknown();

      if (ch === '\\') {
        const next = src[i + 1];
        if (next === undefined) unknown();
        i += 2;

        if (isAlnum(next)) {
          opaque = true;
          return -1;
        }

        return next.charCodeAt(0);
      }

      i++;
      return ch.charCodeAt(0);
    };

    while (src[i] !== ']') {
      const lo = readOne();

      if (src[i] === '-' && src[i + 1] !== ']' && src[i + 1] !== undefined) {
        i++;
        const hi = readOne();

        if (lo < 0 || hi < 0) {
          opaque = true;
        } else if (lo > hi) {
          unknown();
        } else {
          ranges.push([lo, hi]);
        }
      } else if (lo >= 0) {
        ranges.push([lo, lo]);
      }
    }

    i++;
    return opaque ? { type: 'opaque', pure: false } : makeSet(ranges, negate);
  };

  const parseAtom = () => {
    const ch = src[i];

    if (ch === '(') {
      let kind = 'capture';
      let skip = 1;

      if (src[i + 1] === '?') {
        const next = src[i + 2];

        if (next === ':') {
          kind = 'plain';
          skip = 3;
        } else if (next === '=' || next === '!') {
          kind = 'look';
          skip = 3;
        } else if (next === '<' && (src[i + 3] === '=' || src[i + 3] === '!')) {
          kind = 'look';
          skip = 4;
        } else {
          unknown();
        }
      }

      i += skip;
      const inner = parseAlt();
      if (src[i] !== ')') unknown();
      i++;
      return kind === 'look'
        ? { type: 'look', node: inner, pure: false }
        : { type: 'group', node: inner, pure: inner.pure };
    }

    if (ch === '[') return parseClass();

    if (ch === '\\') {
      const next = src[i + 1];
      if (next === undefined) unknown();
      i += 2;
      return isAlnum(next) ? { type: 'opaque', pure: false } : makeSet([[next.charCodeAt(0), next.charCodeAt(0)]], false);
    }

    if (ch === '^' || ch === '$') {
      i++;
      return { type: 'opaque', pure: false };
    }

    if (ch === '.') {
      i++;
      return makeSet([[0x0a, 0x0a], [0x0d, 0x0d], [0x2028, 0x2029]], true);
    }

    if (ch === '*' || ch === '+' || ch === '?' || ch === '{' || ch === '}' || ch === ']' || ch === ')' || ch === '|') {
      unknown();
    }

    i++;
    return makeSet([[ch.charCodeAt(0), ch.charCodeAt(0)]], false);
  };

  const parseTerm = () => {
    const start = i;
    let atom = parseAtom();
    atom.start = start;
    atom.end = i;
    const q = src[i];

    if (q === '*' || q === '+' || q === '?') {
      i++;
      if (src[i] === '?') i++;
      atom = {
        type: 'rep',
        node: atom,
        min: q === '+' ? 1 : 0,
        max: q === '?' ? 1 : Infinity,
        pure: atom.pure,
        start,
        end: i
      };
    } else if (q === '{') {
      // a counted repeat is read for its structure only; a group that holds one is not analysed
      const counted = /^\{\d+(?:,\d*)?\}/.exec(src.slice(i, i + 24));
      if (!counted) unknown();
      i += counted[0].length;
      if (src[i] === '?') i++;
      atom = { type: 'count', node: atom, pure: false, start, end: i };
    }

    return atom;
  };

  const parseCat = () => {
    const start = i;
    const items = [];

    while (i < src.length && src[i] !== '|' && src[i] !== ')') items.push(parseTerm());

    return { type: 'cat', items, pure: items.every(item => item.pure), start, end: i };
  };

  const parseAlt = () => {
    const start = i;
    const items = [parseCat()];

    while (src[i] === '|') {
      i++;
      items.push(parseCat());
    }

    if (items.length === 1) return items[0];
    return { type: 'alt', items, pure: items.every(item => item.pure), start, end: i };
  };

  const root = parseAlt();
  if (i !== src.length) unknown();
  return root;
};

/* ---------- position automaton, ambiguity test ---------- */

// Position automaton that also counts, for every move, in how many different ways the expression can make it (capped at
// 2). `(b+)+` moves from b to b once through the inner loop and once through the outer one: two ways, which a plain
// position automaton would merge into one, and which is what makes a backtracking engine try every split.
const glushkov = root => {
  const sets = [];
  const follow = [];

  const bump = (map, key, by) => map.set(key, Math.min(2, (map.get(key) || 0) + by));

  const scaled = (map, times) => {
    const out = new Map();
    if (times > 0) for (const [key, count] of map) out.set(key, Math.min(2, count * times));
    return out;
  };

  const merge = (a, b) => {
    const out = new Map(a);
    for (const [key, count] of b) bump(out, key, count);
    return out;
  };

  const link = (from, to) => {
    for (const [p, a] of from) for (const [q, b] of to) bump(follow[p], q, a * b);
  };

  // { n: ways to match the empty text, F: ways to enter at each position, L: ways to leave from each position }
  const walk = node => {
    switch (node.type) {
      case 'set': {
        if (sets.length >= LIMIT.positions) unknown();
        sets.push(node.set);
        follow.push(new Map());
        const p = sets.length - 1;
        return { n: 0, F: new Map([[p, 1]]), L: new Map([[p, 1]]) };
      }

      case 'group':
        return walk(node.node);

      case 'cat': {
        let acc = { n: 1, F: new Map(), L: new Map() };

        for (const item of node.items) {
          const r = walk(item);
          link(acc.L, r.F);
          acc = {
            n: Math.min(2, acc.n * r.n),
            F: merge(acc.F, scaled(r.F, acc.n)),
            L: merge(r.L, scaled(acc.L, r.n))
          };
        }

        return acc;
      }

      case 'alt': {
        const parts = node.items.map(walk);
        return {
          n: Math.min(2, parts.reduce((sum, part) => sum + part.n, 0)),
          F: parts.reduce((all, part) => merge(all, part.F), new Map()),
          L: parts.reduce((all, part) => merge(all, part.L), new Map())
        };
      }

      case 'rep': {
        const r = walk(node.node);
        if (node.max === Infinity) link(r.L, r.F);
        // an iteration that matches nothing is rejected by the engine once the minimum is met
        return { n: node.min === 0 ? 1 : r.n, F: r.F, L: r.L };
      }

      default:
        return unknown();
    }
  };

  const top = walk(root);
  return {
    sets,
    follow: follow.map(map => [...map.keys()]),
    multi: follow.map(map => new Set([...map].filter(([, count]) => count >= 2).map(([q]) => q))),
    first: [...top.F.keys()],
    last: [...top.L.keys()],
    nullable: top.n > 0
  };
};

const buildCells = sets => {
  const points = new Set([0]);

  for (const set of sets) {
    for (const [lo, hi] of set) {
      points.add(lo);
      if (hi + 1 <= MAX) points.add(hi + 1);
    }
  }

  const sorted = [...points].sort((a, b) => a - b);
  const all = sorted.map((lo, k) => [lo, (k + 1 < sorted.length ? sorted[k + 1] : MAX + 1) - 1]);
  const index = new Map(all.map((cell, k) => [cell[0], k]));
  const member = sets.map(() => new Uint8Array(all.length));
  const used = new Uint8Array(all.length);

  sets.forEach((set, p) => {
    for (const [lo, hi] of set) {
      for (let k = index.get(lo); k < all.length && all[k][0] <= hi; k++) {
        member[p][k] = 1;
        used[k] = 1;
      }
    }
  });

  const keep = [];
  all.forEach((cell, k) => {
    if (used[k]) keep.push(k);
  });

  if (keep.length > LIMIT.cells) unknown();

  return {
    cells: keep.map(k => all[k]),
    masks: member.map(row => Uint8Array.from(keep, k => row[k]))
  };
};

// Positions that can be reached from the start and can reach an accepting position.
const usablePositions = (auto, masks) => {
  const n = auto.sets.length;
  const alive = masks.map(mask => mask.some(Boolean));
  const reach = new Uint8Array(n);
  const stack = auto.first.filter(p => alive[p]);
  stack.forEach(p => {
    reach[p] = 1;
  });

  while (stack.length > 0) {
    const p = stack.pop();

    for (const q of auto.follow[p]) {
      if (alive[q] && !reach[q]) {
        reach[q] = 1;
        stack.push(q);
      }
    }
  }

  const back = Array.from({ length: n }, () => []);
  auto.follow.forEach((list, p) => list.forEach(q => back[q].push(p)));
  const co = new Uint8Array(n);
  const todo = auto.last.filter(p => reach[p]);
  todo.forEach(p => {
    co[p] = 1;
  });

  while (todo.length > 0) {
    const q = todo.pop();

    for (const p of back[q]) {
      if (reach[p] && !co[p]) {
        co[p] = 1;
        todo.push(p);
      }
    }
  }

  return Array.from({ length: n }, (_, p) => reach[p] && co[p] === 1);
};

const makeBudget = () => {
  let work = 0;
  return amount => {
    work += amount;
    if (work > LIMIT.work) unknown();
  };
};

// Exponential ambiguity: a state with two different paths back to itself on the same word.
const isExponentiallyAmbiguous = (auto, masks, usable, spend) => {
  const n = auto.sets.length;
  const follow = auto.follow.map((list, p) => (usable[p] ? list.filter(q => usable[q]) : []));
  const overlapCache = new Map();

  const overlap = (p, q) => {
    const key = p * n + q;
    let value = overlapCache.get(key);

    if (value === undefined) {
      spend(masks[p].length);
      value = false;
      for (let k = 0; k < masks[p].length; k++) {
        if (masks[p][k] && masks[q][k]) {
          value = true;
          break;
        }
      }
      overlapCache.set(key, value);
    }

    return value;
  };

  const neighbours = new Map();

  const next = node => {
    let list = neighbours.get(node);

    if (!list) {
      const p = Math.floor(node / n);
      const q = node % n;
      list = [];
      spend(follow[p].length * follow[q].length + 1);

      for (const p2 of follow[p]) {
        for (const q2 of follow[q]) {
          if (overlap(p2, q2)) list.push(p2 * n + q2);
        }
      }

      neighbours.set(node, list);
    }

    return list;
  };

  // Tarjan's strongly connected components, iterative, from every diagonal pair.
  const indexOf = new Map();
  const lowOf = new Map();
  const onStack = new Set();
  const stack = [];
  let counter = 0;
  let found = false;

  const component = members => {
    const inside = new Set(members);
    let diagonal = false;
    let off = false;
    let twice = false;

    for (const node of members) {
      const p = Math.floor(node / n);
      const q = node % n;

      if (p !== q) {
        off = true;
        continue;
      }

      diagonal = true;

      for (const to of next(node)) {
        const p2 = Math.floor(to / n);
        if (to % n === p2 && inside.has(to) && auto.multi[p].has(p2)) twice = true;
      }
    }

    if (diagonal && (off || twice)) found = true;
  };

  for (let p = 0; p < n && !found; p++) {
    if (!usable[p] || indexOf.has(p * n + p)) continue;
    const frames = [{ node: p * n + p, at: 0 }];
    indexOf.set(p * n + p, counter);
    lowOf.set(p * n + p, counter);
    counter++;
    stack.push(p * n + p);
    onStack.add(p * n + p);

    while (frames.length > 0 && !found) {
      const frame = frames[frames.length - 1];
      const list = next(frame.node);

      if (frame.at < list.length) {
        const to = list[frame.at++];

        if (!indexOf.has(to)) {
          indexOf.set(to, counter);
          lowOf.set(to, counter);
          counter++;
          stack.push(to);
          onStack.add(to);
          frames.push({ node: to, at: 0 });
        } else if (onStack.has(to)) {
          lowOf.set(frame.node, Math.min(lowOf.get(frame.node), indexOf.get(to)));
        }
      } else {
        frames.pop();

        if (frames.length > 0) {
          const parent = frames[frames.length - 1].node;
          lowOf.set(parent, Math.min(lowOf.get(parent), lowOf.get(frame.node)));
        }

        if (lowOf.get(frame.node) === indexOf.get(frame.node)) {
          const members = [];
          let top;

          do {
            top = stack.pop();
            onStack.delete(top);
            members.push(top);
          } while (top !== frame.node);

          component(members);
        }
      }
    }
  }

  return found;
};

/* ---------- deterministic automaton, minimized, written back as an expression ---------- */

const buildDfa = (auto, masks, usable, cells, spend) => {
  const ids = new Map();
  auto.sets.forEach((_, p) => {
    if (usable[p]) ids.set(p, ids.size);
  });

  const START = ids.size;
  const successors = [];
  const accepting = new Set();

  for (const [p, id] of ids) {
    successors[id] = auto.follow[p].filter(q => ids.has(q)).map(q => ids.get(q));
    if (auto.last.includes(p)) accepting.add(id);
  }

  successors[START] = auto.first.filter(p => ids.has(p)).map(p => ids.get(p));
  if (auto.nullable) accepting.add(START);
  const maskOf = [];
  for (const [p, id] of ids) maskOf[id] = masks[p];

  const keyToState = new Map();
  const members = [];
  const trans = [];

  const intern = set => {
    const key = set.join(',');
    let id = keyToState.get(key);

    if (id === undefined) {
      if (members.length >= LIMIT.states) unknown();
      id = members.length;
      keyToState.set(key, id);
      members.push(set);
      trans.push(new Int32Array(cells.length).fill(-1));
    }

    return id;
  };

  intern([START]);

  for (let s = 0; s < members.length; s++) {
    spend(cells.length * (members[s].length + 1));

    for (let c = 0; c < cells.length; c++) {
      const target = new Set();

      for (const p of members[s]) {
        for (const q of successors[p]) {
          if (maskOf[q][c]) target.add(q);
        }
      }

      if (target.size > 0) trans[s][c] = intern([...target].sort((a, b) => a - b));
    }
  }

  const isAccept = members.map(set => set.some(p => accepting.has(p)));

  // keep only states that can still reach an accepting state
  const back = members.map(() => []);
  trans.forEach((row, s) => row.forEach(t => t >= 0 && back[t].push(s)));
  const live = new Uint8Array(members.length);
  const todo = [];
  isAccept.forEach((yes, s) => {
    if (yes) {
      live[s] = 1;
      todo.push(s);
    }
  });

  while (todo.length > 0) {
    const t = todo.pop();

    for (const s of back[t]) {
      if (!live[s]) {
        live[s] = 1;
        todo.push(s);
      }
    }
  }

  trans.forEach(row => {
    for (let c = 0; c < row.length; c++) if (row[c] >= 0 && !live[row[c]]) row[c] = -1;
  });

  // Moore minimization
  let klass = members.map((_, s) => (isAccept[s] ? 1 : 0));
  let count = new Set(klass).size;

  for (;;) {
    spend(members.length * cells.length);
    const signatures = new Map();
    const next = members.map((_, s) => {
      const row = trans[s];
      let key = `${klass[s]}`;
      for (let c = 0; c < cells.length; c++) key += `,${row[c] < 0 ? -1 : klass[row[c]]}`;
      if (!signatures.has(key)) signatures.set(key, signatures.size);
      return signatures.get(key);
    });

    klass = next;
    if (signatures.size === count) break;
    count = signatures.size;
  }

  const representative = new Map();
  klass.forEach((k, s) => {
    if (live[s] && !representative.has(k)) representative.set(k, s);
  });

  const classes = [...representative.keys()];
  const renumber = new Map(classes.map((k, idx) => [k, idx]));
  const edges = classes.map(() => new Map());

  for (const [k, s] of representative) {
    const from = renumber.get(k);

    for (let c = 0; c < cells.length; c++) {
      const t = trans[s][c];
      if (t < 0) continue;
      const to = renumber.get(klass[t]);
      if (!edges[from].has(to)) edges[from].set(to, []);
      edges[from].get(to).push(cells[c]);
    }
  }

  return {
    start: live[0] ? renumber.get(klass[0]) : -1,
    accept: classes.map(k => isAccept[representative.get(k)]),
    edges: edges.map(row => new Map([...row].map(([to, ranges]) => [to, normalize(ranges)])))
  };
};

const escapeOutside = c => {
  if (/[A-Za-z0-9_ ]/.test(String.fromCharCode(c))) return String.fromCharCode(c);
  if (c >= 0x21 && c <= 0x7e) return `\\${String.fromCharCode(c)}`;
  return c < 0x100 ? `\\x${c.toString(16).padStart(2, '0')}` : `\\u${c.toString(16).padStart(4, '0')}`;
};

const escapeInside = c => {
  const ch = String.fromCharCode(c);
  if (c >= 0x20 && c <= 0x7e) return /[\\\]^[-]/.test(ch) ? `\\${ch}` : ch;
  return c < 0x100 ? `\\x${c.toString(16).padStart(2, '0')}` : `\\u${c.toString(16).padStart(4, '0')}`;
};

const classSource = ranges => {
  const total = size(ranges);
  if (total === MAX + 1) return '[^]';
  if (total === 1) return escapeOutside(ranges[0][0]);

  const body = list =>
    list
      .map(([lo, hi]) => {
        if (lo === hi) return escapeInside(lo);
        if (lo + 1 === hi) return escapeInside(lo) + escapeInside(hi);
        return `${escapeInside(lo)}-${escapeInside(hi)}`;
      })
      .join('');

  return total > (MAX + 1) / 2 ? `[^${body(complement(ranges))}]` : `[${body(ranges)}]`;
};

// Expressions while eliminating states: prec 0 alternation, 1 sequence, 2 a single quantifiable unit.
const EPS = { src: '', prec: 1, eps: true };
const wrap = (x, least) => (x.prec >= least ? x.src : `(?:${x.src})`);

const seq = (a, b) => {
  if (!a || !b) return null;
  if (a.eps) return b;
  if (b.eps) return a;
  return { src: wrap(a, 1) + wrap(b, 1), prec: 1 };
};

const star = x => {
  if (x.eps) return EPS;
  if (x.star) return x;
  return { src: `${x.quant ? `(?:${x.src})` : wrap(x, 2)}*`, prec: 2, quant: true, star: true };
};

const alt = (a, b) => {
  if (!a) return b;
  if (!b) return a;

  if (a.eps || b.eps) {
    const x = a.eps ? b : a;
    if (x.eps) return EPS;
    return { src: `${x.quant ? `(?:${x.src})` : wrap(x, 2)}?`, prec: 2, quant: true };
  }

  return { src: `${a.src}|${b.src}`, prec: 0 };
};

const toExpression = (dfa, spend) => {
  if (dfa.start < 0) return '[]';

  const count = dfa.accept.length;
  const S = count;
  const F = count + 1;
  const out = Array.from({ length: count + 2 }, () => new Map());
  const into = Array.from({ length: count + 2 }, () => new Set());

  const put = (from, to, label) => {
    const old = out[from].get(to);
    out[from].set(to, old ? alt(old, label) : label);
    into[to].add(from);
  };

  put(S, dfa.start, EPS);
  dfa.accept.forEach((yes, s) => yes && put(s, F, EPS));
  dfa.edges.forEach((row, from) => row.forEach((ranges, to) => put(from, to, { src: classSource(ranges), prec: 2 })));

  const remaining = new Set(Array.from({ length: count }, (_, s) => s));

  while (remaining.size > 0) {
    let best = -1;
    let bestCost = Infinity;

    // Eliminate the state whose removal adds the least text: every path through it is copied once per pair of
    // neighbours (Delgado and Morais).
    for (const k of remaining) {
      const ins = [...into[k]].filter(i => i !== k);
      const outs = [...out[k].keys()].filter(j => j !== k);
      const loop = out[k].has(k) ? out[k].get(k).src.length : 0;
      let cost = loop * (ins.length * outs.length - 1);
      for (const i of ins) cost += out[i].get(k).src.length * (outs.length - 1);
      for (const j of outs) cost += out[k].get(j).src.length * (ins.length - 1);

      if (cost < bestCost) {
        best = k;
        bestCost = cost;
      }
    }

    const k = best;
    const loop = out[k].get(k);
    const again = loop ? star(loop) : EPS;
    spend(Math.min(bestCost, 100000) + 1);

    for (const i of [...into[k]]) {
      if (i === k) continue;

      for (const [j, after] of [...out[k]]) {
        if (j === k) continue;
        const path = seq(seq(out[i].get(k), again), after);
        put(i, j, path);
        if (path.src.length > LIMIT.output) unknown();
      }
    }

    for (const i of [...into[k]]) out[i].delete(k);
    for (const j of out[k].keys()) into[j].delete(k);
    out[k].clear();
    into[k].clear();
    remaining.delete(k);
  }

  const result = out[S].get(F);
  if (!result) return '[]';
  if (result.src.length > LIMIT.output) unknown();
  return result.src;
};

/* ---------- finding repeats in a source and replacing the ambiguous ones ---------- */

const isSingleUnit = node => {
  let cur = node;
  while (cur.type === 'group' || (cur.type === 'cat' && cur.items.length === 1)) cur = cur.type === 'group' ? cur.node : cur.items[0];
  return cur.type === 'set';
};

const rewriteRepeat = (rep, all) => {
  const auto = glushkov(rep);
  const spend = makeBudget();
  const { cells, masks } = buildCells(auto.sets);
  const usable = usablePositions(auto, masks);

  if (!all && !isExponentiallyAmbiguous(auto, masks, usable, spend)) return null;

  return `(?:${toExpression(buildDfa(auto, masks, usable, cells, spend), spend)})`;
};

const children = node => {
  switch (node.type) {
    case 'cat':
    case 'alt':
      return node.items;
    case 'group':
    case 'rep':
    case 'look':
    case 'count':
      return [node.node];
    default:
      return [];
  }
};

/**
 * Replace every exponentially ambiguous repeat in a regular expression source.
 * @param {String} source Regular expression source.
 * @param {String} flags Regular expression flags.
 * @param {Object} [options] `all: true` rewrites every repeat of a group, ambiguous or not (used by the tests).
 * @return {{source: String, rewritten: Number, unknown: Number}}
 */
const transform = (source, flags, options = {}) => {
  flags = typeof flags === 'string' ? flags : '';
  const result = { source, rewritten: 0, unknown: 0 };

  if (typeof source !== 'string' || source.length > LIMIT.source || !/\)[*+]/.test(source)) return result;
  if (/[uvms]/.test(flags)) return result;

  let root;

  try {
    root = parseSource(source, flags.includes('i'));
  } catch (err) {
    result.unknown = 1;
    return result;
  }

  const replacements = [];

  const visit = node => {
    if (node.type === 'rep' && node.max === Infinity && node.pure && !isSingleUnit(node.node)) {
      try {
        const text = rewriteRepeat(node, options.all === true);

        if (text !== null) {
          replacements.push({ start: node.start, end: node.end, text });
          return;
        }

        if (options.all !== true) return;
      } catch (err) {
        if (!(err instanceof Unknown)) throw err;
        result.unknown++;
      }
    }

    for (const child of children(node)) visit(child);
  };

  try {
    visit(root);
  } catch (err) {
    return { source, rewritten: 0, unknown: result.unknown + 1 };
  }

  let out = source;

  for (const { start, end, text } of replacements.sort((a, b) => b.start - a.start)) {
    out = out.slice(0, start) + text + out.slice(end);
  }

  try {
    new RegExp(out, flags); // eslint-disable-line no-new
  } catch (err) {
    return { source, rewritten: 0, unknown: result.unknown + 1 };
  }

  return { source: out, rewritten: replacements.length, unknown: result.unknown };
};

module.exports = { transform };
