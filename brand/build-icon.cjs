/* Lamp Guide 图标：单一来源。32×32 网格手写几何，
   改这里，SVG / Android 矢量 / PNG / 联系表全部重生成。
   形态沿用桌面版 web/assets/lamp-guide.svg 的语言：半圆提手 + 锥形灯罩 + 灯身 + 灯芯，
   但把描边全部改成面，画面才在 16px 上不糊。 */
const fs = require('node:fs');
const path = require('node:path');
const { Resvg } = require('@resvg/resvg-js');

const ROOT = path.resolve(__dirname, '..');
const BRAND = path.join(ROOT, 'brand');
const RES = path.join(ROOT, 'android/app-native/app/src/main/res');
const OUT = path.join(BRAND, 'out');

const num = (v) => String(Math.round(v * 1000) / 1000);
const unit = { X: (x) => num(x), Y: (y) => num(y), L: (v) => num(v) };

/** 32 网格 → 108dp 自适应图标视口：整体 ×3.5，中心 (16,16)→(54,54)。 */
const AND = {
  X: (x) => num((x - 16) * 3.5 + 54),
  Y: (y) => num((y - 16) * 3.5 + 54),
  L: (v) => num(v * 3.5),
};

/** 圆角矩形 → path。 */
function rr(f, x, y, w, h, r) {
  const rad = Math.min(r, w / 2, h / 2), x2 = x + w, y2 = y + h, a = f.L(rad);
  return 'M' + f.X(x + rad) + ',' + f.Y(y) +
    ' L' + f.X(x2 - rad) + ',' + f.Y(y) +
    'A' + a + ',' + a + ' 0 0 1 ' + f.X(x2) + ',' + f.Y(y + rad) +
    ' L' + f.X(x2) + ',' + f.Y(y2 - rad) +
    'A' + a + ',' + a + ' 0 0 1 ' + f.X(x2 - rad) + ',' + f.Y(y2) +
    ' L' + f.X(x + rad) + ',' + f.Y(y2) +
    'A' + a + ',' + a + ' 0 0 1 ' + f.X(x) + ',' + f.Y(y2 - rad) +
    ' L' + f.X(x) + ',' + f.Y(y + rad) +
    'A' + a + ',' + a + ' 0 0 1 ' + f.X(x + rad) + ',' + f.Y(y) + 'Z';
}

/** 梯形，底角可圆。sides 取 0（直边）到 1（内凹）。 */
function taper(f, o) {
  const top = o.top, bottom = o.bottom, ht = o.topHalf, hb = o.bottomHalf, rad = o.r || 0;
  const r = Math.min(rad, hb, (bottom - top) / 2);
  const side = hb + (ht - hb) * r / (bottom - top);
  return 'M' + f.X(16 - ht) + ',' + f.Y(top) +
    ' L' + f.X(16 + ht) + ',' + f.Y(top) +
    ' L' + f.X(16 + side) + ',' + f.Y(bottom - r) +
    ' Q' + f.X(16 + hb) + ',' + f.Y(bottom) + ' ' + f.X(16 + hb - r) + ',' + f.Y(bottom) +
    ' L' + f.X(16 - hb + r) + ',' + f.Y(bottom) +
    ' Q' + f.X(16 - hb) + ',' + f.Y(bottom) + ' ' + f.X(16 - side) + ',' + f.Y(bottom - r) +
    ' L' + f.X(16 - ht) + ',' + f.Y(top) + 'Z';
}

/** 灯芯：上尖下圆的一滴，底部半圆用弧补。 */
function flame(f, o) {
  const cx = o.cx, top = o.top, bottom = o.bottom, half = o.half, a = f.L(half);
  return 'M' + f.X(cx) + ',' + f.Y(top) +
    ' C' + f.X(cx - half * 0.6) + ',' + f.Y(top + 1.5) +
    ' ' + f.X(cx - half) + ',' + f.Y(top + 2.4) +
    ' ' + f.X(cx - half) + ',' + f.Y(bottom - half) +
    'A' + a + ',' + a + ' 0 0 0 ' + f.X(cx + half) + ',' + f.Y(bottom - half) +
    ' C' + f.X(cx + half) + ',' + f.Y(top + 2.4) +
    ' ' + f.X(cx + half * 0.6) + ',' + f.Y(top + 1.5) +
    ' ' + f.X(cx) + ',' + f.Y(top) + 'Z';
}

/* ------------------------------------------------------------------ 几何常数 */
/* 提手弧度：与灯罩顶边相切的半圆。 */
const HANDLE = { x1: 13.1, x2: 18.9, y: 11.6, r: 2.9, w: 1.3 };
/* 锥形灯罩：上窄下宽，底边圆角收一点。 */
const SHADE = { top: 11.4, bottom: 14.4, topHalf: 2.0, bottomHalf: 6.9, r: 0.55 };
/* 灯身：从灯罩下沿接出，向下收，底角圆。 */
const BELL = { top: 14.4, bottom: 21.9, topHalf: 6.75, bottomHalf: 4.5, r: 1.0 };
/* 玻璃：灯身内缩一档，颜色是夜色，灯芯画在里面。 */
const GLASS = { top: 15.4, bottom: 20.9, topHalf: 5.6, bottomHalf: 3.4, r: 0.7 };
const FLAME = { cx: 16, top: 16.1, bottom: 20.2, half: 1.5 };
/* 底座：一条压扁的圆角条，宽度略超灯身底。 */
const BASE = { x: 10.9, y: 21.9, w: 10.2, h: 1.4, r: 0.65 };

/* ------------------------------------------------------------------ 画法 */
function mark(f, o) {
  const handle = o.handle || HANDLE, shade = o.shade || SHADE, bell = o.bell || BELL;
  const glass = o.glass || GLASS, flameO = o.flame || FLAME, base = o.base || BASE;
  const parts = [];
  parts.push('<path d="M' + f.X(handle.x1) + ',' + f.Y(handle.y) + 'A' + f.L(handle.r) + ',' + f.L(handle.r) + ' 0 0 1 ' + f.X(handle.x2) + ',' + f.Y(handle.y) + '" stroke="url(#amber)" stroke-width="' + f.L(handle.w) + '" stroke-linecap="round" fill="none"/>');
  parts.push('<path d="' + taper(f, shade) + '" fill="url(#amber)"/>');
  parts.push('<path d="' + taper(f, bell) + '" fill="url(#amber)"/>');
  if (o.glassFill !== false) parts.push('<path d="' + taper(f, glass) + '" fill="url(#glass)"/>');
  parts.push('<path d="' + flame(f, flameO) + '" fill="#FFF3D2"/>');
  parts.push('<path d="' + rr(f, base.x, base.y, base.w, base.h, base.r) + '" fill="url(#amber)"/>');
  return parts.join('\n  ');
}

/* 只有灯身、没有玻璃时（16px 剪影）把灯芯画满一格，免得中间出现空洞。 */
const SMALL = {
  handle: { x1: 12.4, x2: 19.6, y: 11.8, r: 3.6, w: 1.8 },
  shade: { top: 11.5, bottom: 14.7, topHalf: 2.6, bottomHalf: 7.8, r: 0.7 },
  bell: { top: 14.7, bottom: 22.2, topHalf: 7.6, bottomHalf: 5.4, r: 1.1 },
  glassFill: false,
  flame: { cx: 16, top: 16.0, bottom: 21.2, half: 2.0 },
  base: { x: 9.7, y: 22.2, w: 12.6, h: 1.8, r: 0.85 },
};

/* ------------------------------------------------------------------ defs */
const FULL_DEFS = '<defs>' +
  '<linearGradient id="night" x1="1.5" y1="1.5" x2="30.5" y2="30.5" gradientUnits="userSpaceOnUse">' +
    '<stop stop-color="#2A4C6E"/><stop offset=".55" stop-color="#14293F"/><stop offset="1" stop-color="#081521"/>' +
  '</linearGradient>' +
  '<radialGradient id="glow" cx="16" cy="16" r="12.6" gradientUnits="userSpaceOnUse">' +
    '<stop stop-color="#FFC85F" stop-opacity=".38"/><stop offset=".55" stop-color="#FFB44A" stop-opacity=".12"/><stop offset="1" stop-color="#FFB44A" stop-opacity="0"/>' +
  '</radialGradient>' +
  '<linearGradient id="amber" x1="10" y1="8" x2="22" y2="24" gradientUnits="userSpaceOnUse">' +
    '<stop stop-color="#FFF0C4"/><stop offset=".42" stop-color="#FACA6C"/><stop offset="1" stop-color="#E29A2A"/>' +
  '</linearGradient>' +
  '<linearGradient id="glass" x1="12" y1="14.6" x2="20" y2="20.9" gradientUnits="userSpaceOnUse">' +
    '<stop stop-color="#0B1D2D"/><stop offset="1" stop-color="#050F19"/>' +
  '</linearGradient>' +
'</defs>';

const SMALL_DEFS = '<defs>' +
  '<linearGradient id="night" x1="1" y1="1" x2="31" y2="31" gradientUnits="userSpaceOnUse">' +
    '<stop stop-color="#2A4C6E"/><stop offset="1" stop-color="#0A1826"/>' +
  '</linearGradient>' +
  '<linearGradient id="amber" x1="9" y1="7" x2="23" y2="25" gradientUnits="userSpaceOnUse">' +
    '<stop stop-color="#FFEDB8"/><stop offset="1" stop-color="#E0981F"/>' +
  '</linearGradient>' +
'</defs>';

const TILE = '<rect x="1.5" y="1.5" width="29" height="29" rx="6.8" fill="url(#night)"/>' +
  '<rect x="1.6" y="1.6" width="28.8" height="28.8" rx="6.7" fill="none" stroke="#FFFFFF" stroke-opacity=".14" stroke-width=".12"/>' +
  '<circle cx="16" cy="16" r="12.6" fill="url(#glow)"/>';

const head = (title, comment, defs, body) => '<?xml version="1.0" encoding="UTF-8"?>\n' +
  '<!-- ' + comment + ' -->\n' +
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" fill="none" role="img" aria-label="' + title + '">\n' +
  '  <title>' + title + '</title>\n  ' + defs + '\n' + body + '\n</svg>\n';

const master = head('Lamp Guide',
  'Lamp Guide 应用图标：32×32 网格手写几何，brand/build-icon.cjs 生成，勿手改。',
  FULL_DEFS, '  ' + TILE + '\n  ' + mark(unit, {}));

const smallSvg = head('Lamp Guide',
  '16–32px 专用：去掉玻璃与光晕，笔画加粗、主体放大，缩到 16px 仍是一只灯。',
  SMALL_DEFS, '  <rect x=".5" y=".5" width="31" height="31" rx="7.6" fill="url(#night)"/>\n  ' + mark(unit, SMALL));

const markSvg = head('Lamp Guide',
  '只留灯本身（透明背景），给自适应图标前景与单色图标用。',
  FULL_DEFS, '  ' + mark(unit, {}));

fs.mkdirSync(OUT, { recursive: true });
fs.writeFileSync(path.join(BRAND, 'lamp-guide-icon.svg'), master);
fs.writeFileSync(path.join(BRAND, 'lamp-guide-icon-small.svg'), smallSvg);
fs.writeFileSync(path.join(BRAND, 'lamp-guide-mark.svg'), markSvg);

/* ------------------------------------------------------- Android 矢量 */
function grad(type, attrs, stops) {
  return '            <gradient\n                android:type="' + type + '"' + attrs + '>\n' +
    stops.map((s) => '                <item android:offset="' + s[0] + '" android:color="' + s[1] + '"/>').join('\n') +
    '\n            </gradient>';
}
const AMBER_ATTRS = '\n                android:startX="' + AND.X(10) + '" android:startY="' + AND.Y(8) + '"' +
  '\n                android:endX="' + AND.X(22) + '" android:endY="' + AND.Y(24) + '"';
const AMBER_STOPS = [['0', '#FFF0C4'], ['0.42', '#FACA6C'], ['1', '#E29A2A']];
const attr = (inner) => '        <aapt:attr name="android:fillColor">\n' + inner + '\n        </aapt:attr>';
const attrStroke = (inner) => '        <aapt:attr name="android:strokeColor">\n' + inner + '\n        </aapt:attr>';

const fg = '<?xml version="1.0" encoding="utf-8"?>\n' +
  '<!--\n  Lamp Guide 自适应图标前景：光晕 + 提手 + 灯罩 + 灯身 + 灯芯 + 底座。\n' +
  '  108dp 视口；提手至底座全部落在中央安全区域内。\n' +
  '  坐标由 brand/build-icon.cjs 从 32 网格 ×3.5 换算，不要手改。\n-->\n' +
  '<vector xmlns:android="http://schemas.android.com/apk/res/android"\n' +
  '    xmlns:aapt="http://schemas.android.com/aapt"\n' +
  '    android:width="108dp" android:height="108dp"\n' +
  '    android:viewportWidth="108" android:viewportHeight="108">\n\n' +
  '    <path android:pathData="M' + AND.X(3.4) + ',' + AND.Y(16) + 'a' + AND.L(12.6) + ',' + AND.L(12.6) + ' 0 1 0 ' + AND.L(25.2) + ',0a' + AND.L(12.6) + ',' + AND.L(12.6) + ' 0 1 0 -' + AND.L(25.2) + ',0z">\n' +
  attr(grad('radial', '\n                android:centerX="' + AND.X(16) + '" android:centerY="' + AND.Y(16) + '" android:gradientRadius="' + AND.L(12.6) + '"', [['0', '#61FFC85F'], ['0.55', '#1FFFB44A'], ['1', '#00FFB44A']])) +
  '\n    </path>\n\n' +
  '    <path android:pathData="M' + AND.X(HANDLE.x1) + ',' + AND.Y(HANDLE.y) + 'A' + AND.L(HANDLE.r) + ',' + AND.L(HANDLE.r) + ' 0 0 1 ' + AND.X(HANDLE.x2) + ',' + AND.Y(HANDLE.y) + '"\n' +
  '        android:fillColor="#00000000" android:strokeWidth="' + AND.L(HANDLE.w) + '" android:strokeLineCap="round">\n' +
  attrStroke(grad('linear', AMBER_ATTRS, AMBER_STOPS)) + '\n    </path>\n\n' +
  '    <path android:pathData="' + taper(AND, SHADE) + '">\n' + attr(grad('linear', AMBER_ATTRS, AMBER_STOPS)) + '\n    </path>\n\n' +
  '    <path android:pathData="' + taper(AND, BELL) + '">\n' + attr(grad('linear', AMBER_ATTRS, AMBER_STOPS)) + '\n    </path>\n\n' +
  '    <path android:pathData="' + taper(AND, GLASS) + '">\n' +
  attr(grad('linear', '\n                android:startX="' + AND.X(12) + '" android:startY="' + AND.Y(GLASS.top) + '"' +
    '\n                android:endX="' + AND.X(20) + '" android:endY="' + AND.Y(GLASS.bottom) + '"', [['0', '#0B1D2D'], ['1', '#050F19']])) +
  '\n    </path>\n\n' +
  '    <path android:fillColor="#FFF3D2" android:pathData="' + flame(AND, FLAME) + '"/>\n\n' +
  '    <path android:pathData="' + rr(AND, BASE.x, BASE.y, BASE.w, BASE.h, BASE.r) + '">\n' + attr(grad('linear', AMBER_ATTRS, AMBER_STOPS)) + '\n    </path>\n' +
'</vector>\n';

function monochromeShapes(f) {
  return '    <path android:fillColor="#00000000" android:strokeColor="#FFFFFFFF" android:strokeWidth="' + f.L(HANDLE.w) + '" android:strokeLineCap="round" android:pathData="M' + f.X(HANDLE.x1) + ',' + f.Y(HANDLE.y) + 'A' + f.L(HANDLE.r) + ',' + f.L(HANDLE.r) + ' 0 0 1 ' + f.X(HANDLE.x2) + ',' + f.Y(HANDLE.y) + '"/>\n' +
    '    <path android:fillColor="#FFFFFFFF" android:pathData="' + taper(f, SHADE) + '"/>\n' +
    '    <!-- evenOdd creates a real transparent window for system tinting. -->\n' +
    '    <path android:fillColor="#FFFFFFFF" android:fillType="evenOdd" android:pathData="' + taper(f, BELL) + ' ' + taper(f, GLASS) + '"/>\n' +
    '    <path android:fillColor="#FFFFFFFF" android:pathData="' + flame(f, FLAME) + '"/>\n' +
    '    <path android:fillColor="#FFFFFFFF" android:pathData="' + rr(f, BASE.x, BASE.y, BASE.w, BASE.h, BASE.r) + '"/>\n';
}

const mono = '<?xml version="1.0" encoding="utf-8"?>\n' +
  '<!-- Android 13+ 主题图标：系统只取 alpha，保留提手、灯窗和灯芯。 -->\n' +
  '<vector xmlns:android="http://schemas.android.com/apk/res/android"\n' +
  '    android:width="108dp" android:height="108dp"\n' +
  '    android:viewportWidth="108" android:viewportHeight="108">\n' +
  monochromeShapes(AND) +
'</vector>\n';

const bg = '<?xml version="1.0" encoding="utf-8"?>\n' +
  '<!-- 图标底色：夜色渐变，alpha 全满；灯与光晕在前景。 -->\n' +
  '<vector xmlns:android="http://schemas.android.com/apk/res/android"\n' +
  '    xmlns:aapt="http://schemas.android.com/aapt"\n' +
  '    android:width="108dp" android:height="108dp"\n' +
  '    android:viewportWidth="108" android:viewportHeight="108">\n' +
  '    <path android:pathData="M0,0h108v108h-108z">\n' +
  attr(grad('linear', '\n                android:startX="6" android:startY="6"\n                android:endX="102" android:endY="102"', [['0', '#2A4C6E'], ['0.55', '#14293F'], ['1', '#081521']])) +
  '\n    </path>\n</vector>\n';

const STAT = {
  X: (x) => num((x - 16) * 1.1 + 12),
  Y: (y) => num((y - 16) * 1.1 + 12),
  L: (v) => num(v * 1.1),
};
const stat = '<?xml version="1.0" encoding="utf-8"?>\n' +
  '<!-- 通知栏小图标：24dp 单色。系统只吃 alpha，所以只画剪影，中间灯芯留空。 -->\n' +
  '<vector xmlns:android="http://schemas.android.com/apk/res/android"\n' +
  '    android:width="24dp" android:height="24dp"\n' +
  '    android:viewportWidth="24" android:viewportHeight="24">\n' +
  monochromeShapes(STAT) +
'</vector>\n';

const adaptive = '<?xml version="1.0" encoding="utf-8"?>\n' +
  '<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n' +
  '    <background android:drawable="@drawable/ic_launcher_background" />\n' +
  '    <foreground android:drawable="@drawable/ic_launcher_foreground" />\n' +
  '    <monochrome android:drawable="@drawable/ic_launcher_monochrome" />\n' +
'</adaptive-icon>\n';

fs.writeFileSync(path.join(RES, 'drawable/ic_launcher_background.xml'), bg);
fs.writeFileSync(path.join(RES, 'drawable/ic_launcher_foreground.xml'), fg);
fs.writeFileSync(path.join(RES, 'drawable/ic_launcher_monochrome.xml'), mono);
fs.writeFileSync(path.join(RES, 'drawable/ic_stat_lamp.xml'), stat);
fs.writeFileSync(path.join(RES, 'mipmap-anydpi-v26/ic_launcher.xml'), adaptive);
fs.writeFileSync(path.join(RES, 'mipmap-anydpi-v26/ic_launcher_round.xml'), adaptive);

/* ------------------------------------------------------- PNG */
const fullSizes = [512, 256, 192, 144, 128, 96, 72, 64, 48, 40, 36, 32];
for (const s of fullSizes) {
  fs.writeFileSync(path.join(OUT, s + '-full.png'),
    new Resvg(master, { fitTo: { mode: 'width', value: s } }).render().asPng());
}
for (const s of [48, 36, 32, 24, 20, 16]) {
  fs.writeFileSync(path.join(OUT, s + '-small.png'),
    new Resvg(smallSvg, { fitTo: { mode: 'width', value: s } }).render().asPng());
}
fs.writeFileSync(path.join(OUT, 'mark-512.png'),
  new Resvg(markSvg, { fitTo: { mode: 'width', value: 512 } }).render().asPng());
console.log('built: svg 3, android resources 6, png ' + (fullSizes.length + 7));
