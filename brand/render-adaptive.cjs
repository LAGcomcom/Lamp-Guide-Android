const fs = require('node:fs');
const path = require('node:path');
const { Resvg } = require('@resvg/resvg-js');

const BRAND = __dirname;
const OUT = path.join(BRAND, 'out');
const RES = path.resolve(BRAND, '../android/app-native/app/src/main/res/drawable');

/**
 * Android vector drawable（本项目用到的子集）→ SVG。
 * 顺序扫描 path，按出现次序消费 aapt 颜色块，避免跨路径错配。
 */
function convert(xml, prefix) {
  const defs = [], shapes = [];
  const src = xml.replace(/<!--[\s\S]*?-->/g, '');
  const grab = (s, k) => (s.match(new RegExp(k + '="([^"]*)"')) || [, ''])[1];
  /* Android 颜色是 #AARRGGBB，SVG 是 #RRGGBBAA；直接搬会整体变绿，必须换算。 */
  const color = (c) => {
    const m = (c || '').match(/^#([0-9A-Fa-f]{8})$/);
    if (!m) return 'stop-color="' + c + '"';
    const hex = m[1];
    const a = Math.round((parseInt(hex.slice(0, 2), 16) / 255) * 1000) / 1000;
    return 'stop-color="#' + hex.slice(2) + '" stop-opacity="' + a + '"';
  };
  let n = 0, i = 0;
  while (i < src.length) {
    const at = src.indexOf('<path', i);
    if (at < 0) break;
    const gt = src.indexOf('>', at);
    const rawAttrs = src.slice(at + 5, gt);
    const selfClosing = rawAttrs.trimEnd().endsWith('/');
    const attrs = rawAttrs.replace(/\/\s*$/, ' ');
    const bodyEnd = selfClosing ? gt : src.indexOf('</path>', gt);
    const body = selfClosing ? '' : src.slice(gt + 1, bodyEnd);
    i = selfClosing ? gt + 1 : bodyEnd + 7;

    const flat = (c) => {
      if (!c) return c;
      const m = c.match(/^#([0-9A-Fa-f]{8})$/);
      if (!m) return c;
      const hex = m[1], a = Math.round((parseInt(hex.slice(0, 2), 16) / 255) * 1000) / 1000;
      return { hex: '#' + hex.slice(2), a: a };
    };
    let fill = grab(attrs, 'android:fillColor');
    let stroke = grab(attrs, 'android:strokeColor');
    let fillOpacity = '', strokeOpacity = '';
    const ff = flat(fill);
    if (ff && typeof ff === 'object') { fill = ff.hex; fillOpacity = ff.a; }
    const ss = flat(stroke);
    if (ss && typeof ss === 'object') { stroke = ss.hex; strokeOpacity = ss.a; }
    const blockRe = /<aapt:attr name="android:(fillColor|strokeColor)">([\s\S]*?)<\/aapt:attr>/g;
    let m;
    while ((m = blockRe.exec(body))) {
      const kind = m[1], inner = m[2];
      const g = inner.match(/<gradient([\s\S]*?)>([\s\S]*?)<\/gradient>/);
      const a = g[1];
      const items = [];
      const itemRe = /<item android:offset="([^"]*)" android:color="([^"]*)"/g;
      let it;
      while ((it = itemRe.exec(g[2]))) items.push('<stop offset="' + it[1] + '" ' + color(it[2]) + '/>');
      const id = prefix + '-g' + n++;
      if (grab(a, 'android:type') === 'radial') {
        defs.push('<radialGradient id="' + id + '" cx="' + grab(a, 'android:centerX') + '" cy="' + grab(a, 'android:centerY') +
          '" r="' + grab(a, 'android:gradientRadius') + '" gradientUnits="userSpaceOnUse">' + items.join('') + '</radialGradient>');
      } else {
        defs.push('<linearGradient id="' + id + '" x1="' + grab(a, 'android:startX') + '" y1="' + grab(a, 'android:startY') +
          '" x2="' + grab(a, 'android:endX') + '" y2="' + grab(a, 'android:endY') + '" gradientUnits="userSpaceOnUse">' + items.join('') + '</linearGradient>');
      }
      if (kind === 'fillColor') fill = 'url(#' + id + ')'; else stroke = 'url(#' + id + ')';
    }

    const d = grab(attrs, 'android:pathData');
    const rule = grab(attrs, 'android:fillType') === 'evenOdd' ? ' fill-rule="evenodd"' : '';
    const sw = grab(attrs, 'android:strokeWidth');
    const cap = grab(attrs, 'android:strokeLineCap');
    shapes.push('<path d="' + d + '"' +
      (fill ? ' fill="' + fill + '"' : ' fill="none"') + rule +
      (fillOpacity !== '' && fill ? ' fill-opacity="' + fillOpacity + '"' : '') +
      (stroke ? ' stroke="' + stroke + '"' : '') +
      (strokeOpacity !== '' && stroke ? ' stroke-opacity="' + strokeOpacity + '"' : '') +
      (sw ? ' stroke-width="' + sw + '"' : '') +
      (cap ? ' stroke-linecap="' + cap + '"' : '') + '/>');
  }
  return { defs: defs.join(''), shapes: shapes.join('') };
}

const read = (f) => fs.readFileSync(path.join(RES, f), 'utf8');
const bg = convert(read('ic_launcher_background.xml'), 'bg');
const fg = convert(read('ic_launcher_foreground.xml'), 'fg');
const mono = convert(read('ic_launcher_monochrome.xml'), 'mono');
const stat = convert(read('ic_stat_lamp.xml'), 'stat');

const svg = (d, s) => '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 108 108"><defs>' + d + '</defs>' + s + '</svg>';
const composed = svg(bg.defs + fg.defs, bg.shapes + fg.shapes);
const monoOnly = svg(mono.defs, mono.shapes);
fs.writeFileSync(path.join(OUT, 'composed-108.svg'), composed);
fs.writeFileSync(path.join(OUT, 'mono-108.svg'), monoOnly);

/* 启动器只显示 108dp 画布中央 72dp，遮罩把外面裁掉；预览要按 108/72 放大再裁。 */
const CROP = 108 / 72;
const cells = [];
[144, 96, 48].forEach((size) => {
  const render = Math.round(size * CROP);
  ['circle', 'squircle', 'square'].forEach((mask) => {
    const png = new Resvg(composed, { fitTo: { mode: 'width', value: render } }).render().asPng();
    const file = path.join(OUT, 'adaptive-' + mask + '-' + size + '.png');
    fs.writeFileSync(file, png);
    const radius = mask === 'circle' ? '50%' : mask === 'squircle' ? Math.round(size * 0.235) + 'px' : '0px';
    const off = -(render - size) / 2;
    cells.push('<div class="c"><div class="mask" style="width:' + size + 'px;height:' + size + 'px;border-radius:' + radius + '">' +
      '<img src="out/' + path.basename(file) + '" style="width:' + render + 'px;height:' + render + 'px;margin:' + off + 'px 0 0 ' + off + 'px">' +
      '</div><span>' + mask + ' ' + size + '</span></div>');
  });
});

const monoRender = Math.round(96 * CROP);
fs.writeFileSync(path.join(OUT, 'adaptive-mono.png'),
  new Resvg(monoOnly, { fitTo: { mode: 'width', value: monoRender } }).render().asPng());
const statSvg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><defs>' + stat.defs + '</defs>' + stat.shapes + '</svg>';
fs.writeFileSync(path.join(OUT, 'stat-24.svg'), statSvg);
fs.writeFileSync(path.join(OUT, 'stat.png'), new Resvg(statSvg, { fitTo: { mode: 'width', value: 96 } }).render().asPng());

const monoOff = -(monoRender - 96) / 2;
const html = '<!doctype html><meta charset="utf-8"><style>' +
  'body{margin:0;background:#0b1420;color:#8fa6bb;font:10px system-ui;padding:14px}' +
  '.r{display:flex;gap:18px;align-items:flex-end;margin-bottom:12px}' +
  '.c{display:flex;flex-direction:column;gap:5px;align-items:center}' +
  '.mask{overflow:hidden}img{display:block}' +
  '.bar{display:flex;gap:12px;align-items:center;padding:10px 14px;border-radius:10px;background:#f3f3f3;color:#333}' +
  '.tint{display:flex;gap:12px;align-items:center;padding:10px 14px;border-radius:10px;background:#16283c;color:#a9c4dd}' +
  '</style>' +
  '<div class="r">' + cells.join('') + '</div>' +
  '<div class="r">' +
  '<div class="c"><div class="mask" style="width:96px;height:96px;border-radius:50%">' +
  '<img src="out/adaptive-mono.png" style="width:' + monoRender + 'px;height:' + monoRender + 'px;margin:' + monoOff + 'px 0 0 ' + monoOff + 'px">' +
  '</div><span>monochrome（系统上色）</span></div>' +
  '<div class="c"><img src="out/stat.png" width="96" height="96"><span>通知栏 24dp 放大</span></div>' +
  '</div>' +
  '<div class="tint"><img src="out/stat.png" width="24" height="24"><span>通知标题栏实际尺寸（深色）</span></div>' +
  '<div class="bar"><img src="out/stat.png" width="24" height="24" style="filter:invert(1)"><span>通知标题栏实际尺寸（浅色）</span></div>';
fs.writeFileSync(path.join(BRAND, '_adaptive.html'), html);

// Standalone preview with the same crop and masks as the launcher.
const dataUri = (s) => 'data:image/svg+xml;base64,' + Buffer.from(s).toString('base64');
const cropped = (s) => s.replace('viewBox="0 0 108 108"', 'viewBox="18 18 72 72"');
const preview = '<svg xmlns="http://www.w3.org/2000/svg" width="840" height="304" viewBox="0 0 840 304">' +
  '<defs><clipPath id="circle"><circle cx="120" cy="126" r="88"/></clipPath>' +
  '<clipPath id="rounded"><rect x="276" y="38" width="176" height="176" rx="42"/></clipPath>' +
  '<clipPath id="theme"><circle cx="642" cy="126" r="88"/></clipPath>' +
  '<filter id="tint"><feFlood flood-color="#F4B54E"/><feComposite in2="SourceGraphic" operator="in"/></filter></defs>' +
  '<rect width="840" height="304" rx="24" fill="#0B1420"/>' +
  '<image href="' + dataUri(cropped(composed)) + '" x="32" y="38" width="176" height="176" clip-path="url(#circle)"/>' +
  '<image href="' + dataUri(cropped(composed)) + '" x="276" y="38" width="176" height="176" clip-path="url(#rounded)"/>' +
  '<circle cx="642" cy="126" r="88" fill="#233246"/>' +
  '<image href="' + dataUri(cropped(monoOnly)) + '" x="554" y="38" width="176" height="176" filter="url(#tint)" clip-path="url(#theme)"/>' +
  '<g fill="#C0CBD5" font-family="Segoe UI, sans-serif" font-size="17" text-anchor="middle">' +
  '<text x="120" y="255">Round launcher</text><text x="364" y="255">Rounded launcher</text><text x="642" y="255">Themed icon</text></g></svg>';
fs.writeFileSync(path.join(OUT, 'launcher-preview.svg'), preview);
fs.writeFileSync(path.join(OUT, 'launcher-preview.png'), new Resvg(preview, { font: { loadSystemFonts: true } }).render().asPng());
console.log('adaptive preview ok');
