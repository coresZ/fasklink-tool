// Minimal QR encoder (byte mode, ECC level M, versions 1..10).
// No dependencies, no CDN: the panel renders the subscription QR locally so it
// also works offline. Verified against segno for multiple payloads.
//
//   import { qrSvg, qrMatrix } from "./qr.mjs";
//   qrSvg("https://example.com")            -> "<svg ...>"
//   qrMatrix("https://example.com")         -> { size, version, mask, modules }

const ECC_PER_BLOCK = { 1: 10, 2: 16, 3: 26, 4: 18, 5: 24, 6: 16, 7: 18, 8: 22, 9: 22, 10: 26 };

// ECC level M: [blocks, dataCodewordsPerBlock] per version.
const BLOCKS_M = {
  1: [[1, 16]],
  2: [[1, 28]],
  3: [[1, 44]],
  4: [[2, 32]],
  5: [[2, 43]],
  6: [[4, 27]],
  7: [[4, 31]],
  8: [[2, 38], [2, 39]],
  9: [[3, 36], [2, 37]],
  10: [[4, 43], [1, 44]],
};

const ALIGN = {
  1: [], 2: [6, 18], 3: [6, 22], 4: [6, 26], 5: [6, 30],
  6: [6, 34], 7: [6, 22, 38], 8: [6, 24, 42], 9: [6, 26, 46], 10: [6, 28, 50],
};

const VERSION_BITS = {
  7: 0x07c94, 8: 0x085bc, 9: 0x09a99, 10: 0x0a4d3,
};

// ---------------------------------------------------------------- GF(256) / RS
const EXP = new Uint8Array(512);
const LOG = new Uint8Array(256);
(() => {
  let x = 1;
  for (let i = 0; i < 255; i += 1) {
    EXP[i] = x;
    LOG[x] = i;
    x <<= 1;
    if (x & 0x100) x ^= 0x11d;
  }
  for (let i = 255; i < 512; i += 1) EXP[i] = EXP[i - 255];
})();

const gmul = (a, b) => (a === 0 || b === 0 ? 0 : EXP[LOG[a] + LOG[b]]);

function rsGenerator(degree) {
  let poly = [1];
  for (let i = 0; i < degree; i += 1) {
    const next = new Array(poly.length + 1).fill(0);
    for (let j = 0; j < poly.length; j += 1) {
      next[j] ^= poly[j];
      next[j + 1] ^= gmul(poly[j], EXP[i]);
    }
    poly = next;
  }
  return poly;
}

function rsRemainder(data, degree) {
  const gen = rsGenerator(degree);
  const buffer = new Array(data.length + degree).fill(0);
  for (let i = 0; i < data.length; i += 1) buffer[i] = data[i];
  for (let i = 0; i < data.length; i += 1) {
    const factor = buffer[i];
    if (factor === 0) continue;
    for (let j = 0; j < gen.length; j += 1) {
      buffer[i + j] ^= gmul(gen[j], factor);
    }
  }
  return buffer.slice(data.length);
}

// ---------------------------------------------------------------- bit buffer
class Bits {
  constructor() {
    this.bytes = [];
    this.length = 0;
  }
  push(value, count) {
    for (let i = count - 1; i >= 0; i -= 1) {
      this.pushBit(((value >> i) & 1) === 1);
    }
  }
  pushBit(bit) {
    if (this.length % 8 === 0) this.bytes.push(0);
    if (bit) this.bytes[this.bytes.length - 1] |= 0x80 >> (this.length % 8);
    this.length += 1;
  }
}

function dataCodewords(version) {
  // Total data codewords available (before ECC), summed over all blocks.
  return BLOCKS_M[version].reduce((sum, [n, d]) => sum + n * d, 0);
}

function capacityBytes(version) {
  // Max message bytes the version can carry in byte mode.
  const total = dataCodewords(version);
  const lenBits = version <= 9 ? 8 : 16;
  return Math.floor((total * 8 - 4 - lenBits) / 8);
}

function pickVersion(byteLength) {
  for (let v = 1; v <= 10; v += 1) {
    if (byteLength <= capacityBytes(v)) return v;
  }
  throw new Error("二维码内容过长");
}

function encodeData(bytes, version) {
  const bits = new Bits();
  bits.push(0b0100, 4);
  bits.push(bytes.length, version <= 9 ? 8 : 16);
  for (const byte of bytes) bits.push(byte, 8);

  const capacity = dataCodewords(version) * 8;
  const terminator = Math.min(4, capacity - bits.length);
  if (terminator > 0) bits.push(0, terminator);

  while (bits.length % 8 !== 0) bits.pushBit(false);

  const pad = [0xec, 0x11];
  let i = 0;
  while (bits.bytes.length < dataCodewords(version)) {
    bits.bytes.push(pad[i % 2]);
    i += 1;
  }
  return bits.bytes;
}

function interleave(dataCodewords, version) {
  const blocks = BLOCKS_M[version];
  const ecPerBlock = ECC_PER_BLOCK[version];
  const groups = [];
  let offset = 0;
  for (const [count, dataPerBlock] of blocks) {
    for (let b = 0; b < count; b += 1) {
      const chunk = dataCodewords.slice(offset, offset + dataPerBlock);
      offset += dataPerBlock;
      groups.push({ data: chunk, ec: rsRemainder(chunk, ecPerBlock) });
    }
  }

  const out = [];
  const maxData = Math.max(...groups.map((g) => g.data.length));
  for (let i = 0; i < maxData; i += 1) {
    for (const group of groups) {
      if (i < group.data.length) out.push(group.data[i]);
    }
  }
  for (let i = 0; i < ecPerBlock; i += 1) {
    for (const group of groups) out.push(group.ec[i]);
  }
  return out;
}

// --------------------------------------------------------------------- matrix
function blank(size) {
  return Array.from({ length: size }, () => new Array(size).fill(null));
}

// Marks cells that belong to function patterns. Data placement must skip these;
// getting this wrong silently corrupts timing/format cells.
function functionMask(version) {
  const size = version * 4 + 17;
  const mask = Array.from({ length: size }, () => new Array(size).fill(false));
  const mark = (r0, c0, h, w) => {
    for (let r = r0; r < r0 + h; r += 1) {
      for (let c = c0; c < c0 + w; c += 1) {
        if (r >= 0 && c >= 0 && r < size && c < size) mask[r][c] = true;
      }
    }
  };

  // Finder patterns + separators (8x8 including the white border).
  mark(0, 0, 8, 8);
  mark(0, size - 8, 8, 8);
  mark(size - 8, 0, 8, 8);

  // Timing patterns.
  mark(6, 0, 1, size);
  mark(0, 6, size, 1);

  // Alignment patterns (skip the three that would overlap finders).
  const centers = ALIGN[version];
  const last = centers[centers.length - 1];
  for (const row of centers) {
    for (const col of centers) {
      const nearFinder = (row === 6 && col === 6) || (row === 6 && col === last) || (row === last && col === 6);
      if (nearFinder) continue;
      mark(row - 2, col - 2, 5, 5);
    }
  }

  // Format info (both copies) + dark module.
  for (let i = 0; i <= 5; i += 1) mark(i, 8, 1, 1);
  mark(7, 8, 1, 1);
  mark(8, 8, 1, 1);
  mark(8, 7, 1, 1);
  for (let i = 9; i < 15; i += 1) mark(8, 14 - i, 1, 1);
  for (let i = 0; i < 8; i += 1) mark(8, size - 1 - i, 1, 1);
  for (let i = 8; i < 15; i += 1) mark(size - 15 + i, 8, 1, 1);
  mark(size - 8, 8, 1, 1);

  // Version info (version >= 7 only).
  if (version >= 7) {
    for (let i = 0; i < 18; i += 1) {
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      mark(b, a, 1, 1);
      mark(a, b, 1, 1);
    }
  }

  return mask;
}

function placeFinder(modules, row, col) {
  for (let r = -1; r <= 7; r += 1) {
    for (let c = -1; c <= 7; c += 1) {
      const y = row + r;
      const x = col + c;
      if (y < 0 || x < 0 || y >= modules.length || x >= modules.length) continue;
      const inside = r >= 0 && r <= 6 && c >= 0 && c <= 6;
      const dark = inside && ((r === 0 || r === 6 || c === 0 || c === 6) || (r >= 2 && r <= 4 && c >= 2 && c <= 4));
      modules[y][x] = dark;
    }
  }
}

function placeAlignment(modules, version) {
  const centers = ALIGN[version];
  for (const row of centers) {
    for (const col of centers) {
      // Skip the three corners occupied by finder patterns.
      const nearFinder =
        (row === 6 && col === 6) ||
        (row === 6 && col === centers[centers.length - 1]) ||
        (row === centers[centers.length - 1] && col === 6);
      if (nearFinder) continue;
      for (let r = -2; r <= 2; r += 1) {
        for (let c = -2; c <= 2; c += 1) {
          const dark = Math.max(Math.abs(r), Math.abs(c)) !== 1;
          modules[row + r][col + c] = dark;
        }
      }
    }
  }
}

function placeTiming(modules) {
  for (let i = 8; i < modules.length - 8; i += 1) {
    if (modules[6][i] === null) modules[6][i] = i % 2 === 0;
    if (modules[i][6] === null) modules[i][6] = i % 2 === 0;
  }
}

function placeVersion(modules, version) {
  const bits = VERSION_BITS[version];
  if (!bits) return;
  const size = modules.length;
  for (let i = 0; i < 18; i += 1) {
    const bit = ((bits >> i) & 1) === 1;
    const a = size - 11 + (i % 3);
    const b = Math.floor(i / 3);
    modules[a][b] = bit;
    modules[b][a] = bit;
  }
}

const MASK = [
  (r, c) => (r + c) % 2 === 0,
  (r) => r % 2 === 0,
  (_, c) => c % 3 === 0,
  (r, c) => (r + c) % 3 === 0,
  (r, c) => (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0,
  (r, c) => ((r * c) % 2) + ((r * c) % 3) === 0,
  (r, c) => (((r * c) % 2) + ((r * c) % 3)) % 2 === 0,
  (r, c) => (((r * c) % 3) + ((r + c) % 2)) % 2 === 0,
];

function placeData(modules, reserved, codewords, mask) {
  const size = modules.length;
  let row = size - 1;
  let inc = -1;
  let bitIndex = 7;
  let byteIndex = 0;

  for (let col = size - 1; col > 0; col -= 2) {
    if (col === 6) col -= 1;
    for (;;) {
      for (let c = 0; c < 2; c += 1) {
        const x = col - c;
        if (reserved[row][x]) continue;
        let dark = false;
        if (byteIndex < codewords.length) {
          dark = ((codewords[byteIndex] >> bitIndex) & 1) === 1;
        }
        if (MASK[mask](row, x)) dark = !dark;
        modules[row][x] = dark;
        bitIndex -= 1;
        if (bitIndex === -1) {
          byteIndex += 1;
          bitIndex = 7;
        }
      }
      row += inc;
      if (row < 0 || row >= size) {
        row -= inc;
        inc = -inc;
        break;
      }
    }
  }
}

function formatBits(mask) {
  // ECC level M is 0b00; BCH(15,5) with generator 0x537, then XOR 0x5412.
  const data = (0b00 << 3) | mask;
  let rem = data;
  for (let i = 0; i < 10; i += 1) rem = ((rem << 1) ^ ((rem >>> 9) * 0x537)) & 0x7ff;
  return ((data << 10) | rem) ^ 0x5412;
}

function placeFormat(modules, mask) {
  const size = modules.length;
  const bits = formatBits(mask);
  const bit = (i) => ((bits >> i) & 1) === 1;

  // First copy: bits 0..5 down row 8 (col 8 is the vertical half).
  for (let i = 0; i <= 5; i += 1) modules[i][8] = bit(i);
  modules[7][8] = bit(6);
  modules[8][8] = bit(7);
  modules[8][7] = bit(8);
  for (let i = 9; i < 15; i += 1) modules[8][14 - i] = bit(i);

  // Second copy along the top-right and bottom-left.
  for (let i = 0; i < 8; i += 1) modules[8][size - 1 - i] = bit(i);
  for (let i = 8; i < 15; i += 1) modules[size - 15 + i][8] = bit(i);

  modules[size - 8][8] = true; // always-dark module
}

function penalty(modules) {
  const size = modules.length;
  let score = 0;

  const runScore = (line) => {
    let total = 0;
    let run = 1;
    for (let i = 1; i < line.length; i += 1) {
      if (line[i] === line[i - 1]) run += 1;
      else {
        if (run >= 5) total += 3 + (run - 5);
        run = 1;
      }
    }
    if (run >= 5) total += 3 + (run - 5);
    return total;
  };

  for (let r = 0; r < size; r += 1) score += runScore(modules[r]);
  for (let c = 0; c < size; c += 1) score += runScore(modules.map((row) => row[c]));

  for (let r = 0; r < size - 1; r += 1) {
    for (let c = 0; c < size - 1; c += 1) {
      const v = modules[r][c];
      if (v === modules[r][c + 1] && v === modules[r + 1][c] && v === modules[r + 1][c + 1]) score += 3;
    }
  }

  const pattern = [true, false, true, true, true, false, true];
  const hasPattern = (get, start) => {
    for (let i = 0; i < 7; i += 1) if (get(start + i) !== pattern[i]) return false;
    return true;
  };
  for (let r = 0; r < size; r += 1) {
    for (let c = 0; c <= size - 7; c += 1) {
      if (hasPattern((i) => modules[r][i], c)) {
        const before = [c - 1, c - 2, c - 3, c - 4].every((i) => i < 0 || modules[r][i] === false);
        const after = [c + 7, c + 8, c + 9, c + 10].every((i) => i >= size || modules[r][i] === false);
        if (before || after) score += 40;
      }
      if (hasPattern((i) => modules[i][r], c)) {
        const before = [c - 1, c - 2, c - 3, c - 4].every((i) => i < 0 || modules[i][r] === false);
        const after = [c + 7, c + 8, c + 9, c + 10].every((i) => i >= size || modules[i][r] === false);
        if (before || after) score += 40;
      }
    }
  }

  let dark = 0;
  for (const row of modules) for (const cell of row) if (cell) dark += 1;
  const ratio = (dark * 100) / (size * size);
  score += Math.floor(Math.abs(ratio - 50) / 5) * 10;
  return score;
}

export function qrMatrix(text, options = {}) {
  const bytes = Array.from(new TextEncoder().encode(String(text)));
  const version = options.version || pickVersion(bytes.length);
  const data = encodeData(bytes, version);
  const codewords = interleave(data, version);
  const size = version * 4 + 17;

  const build = (mask) => {
    const modules = blank(size);
    placeFinder(modules, 0, 0);
    placeFinder(modules, 0, size - 7);
    placeFinder(modules, size - 7, 0);
    placeAlignment(modules, version);
    placeTiming(modules);
    placeData(modules, functionMask(version), codewords, mask);
    placeFormat(modules, mask);
    placeVersion(modules, version);
    return modules;
  };

  const forced = Number.isInteger(options.mask) ? options.mask : null;
  if (forced !== null) {
    return { size, version, mask: forced, modules: build(forced).map((row) => row.map(Boolean)) };
  }

  let best = null;
  for (let mask = 0; mask < 8; mask += 1) {
    const modules = build(mask);
    const score = penalty(modules);
    if (!best || score < best.score) best = { score, mask, modules };
  }

  return { size, version, mask: best.mask, modules: best.modules.map((row) => row.map(Boolean)) };
}

export function qrSvg(text, options = {}) {
  const { size, modules } = qrMatrix(text, options);
  const scale = options.scale || 6;
  const margin = options.margin === undefined ? 2 : options.margin;
  const dark = options.dark || "#0f172a";
  const light = options.light || "#ffffff";
  const dim = (size + margin * 2) * scale;

  const rects = [];
  for (let r = 0; r < size; r += 1) {
    let runStart = -1;
    for (let c = 0; c <= size; c += 1) {
      const on = c < size && modules[r][c];
      if (on && runStart < 0) runStart = c;
      if (!on && runStart >= 0) {
        rects.push(
          `<rect x="${(runStart + margin) * scale}" y="${(r + margin) * scale}" width="${(c - runStart) * scale}" height="${scale}"/>`
        );
        runStart = -1;
      }
    }
  }

  return [
    `<svg xmlns="http://www.w3.org/2000/svg" width="${dim}" height="${dim}" viewBox="0 0 ${dim} ${dim}" shape-rendering="crispEdges">`,
    `<rect width="${dim}" height="${dim}" fill="${light}"/>`,
    `<g fill="${dark}">`,
    rects.join(""),
    "</g></svg>",
  ].join("");
}
