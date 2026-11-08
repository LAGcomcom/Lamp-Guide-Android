/* mobile.js —— 安卓宿主上的布局层。

   它只做两件事，都不改桌面版的任何一行：
   1. 侧栏改成抽屉：窄屏下 .pane 从占据布局的一列变成滑出的浮层，页头多一个
      菜单键；宽屏（横屏平板）保持原来的并排布局。
   2. 桌面专属的项收起来：安卓没有注册表 Run 项、没有桌面通知通道、没有可
      原地替换的 exe，那几张卡片在手机上是假的。

   通信不在这一层：bridge.js 的 window.chrome.webview.postMessage 由 Kotlin
   宿主注入（addJavascriptInterface 的 LampAndroid.post），PickSavePath /
   PickOpenPath / OpenPath / OpenUrl 这四个本机动作也由宿主直接回答——所以
   这里不需要替换 postMessage，也不需要伪造信封。

   这个文件由 index.html 在 shell.js 之前加载。 */

(function () {
  'use strict';

  var html = document.documentElement;
  var DRAWER_MIN = 900; /* 低于它就把侧栏收进抽屉（与桌面版紧凑断点 920 对齐） */

  /* ---------------- 1. 抽屉 ---------------- */

  function isDrawer() { return window.innerWidth < DRAWER_MIN; }

  function setDrawer(open) {
    html.setAttribute('data-lg-drawer', open ? 'open' : 'closed');
  }

  var scrim = document.createElement('div');
  scrim.className = 'lg-scrim';
  scrim.addEventListener('click', function () { setDrawer(false); });

  /* shell.js 的 boot() 在同一拍里把 #app 换进 DOM；等它换完再插菜单键，
     否则节点被 replaceWith 丢掉。 */
  function mountMenu() {
    var bar = document.querySelector('.titlebar');
    if (!bar || bar.querySelector('.lg-menu')) return false;

    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'tb-btn lg-menu';
    btn.setAttribute('aria-label', '打开导航');
    btn.innerHTML =
      '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" ' +
      'stroke-linecap="round"><path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h11"/></svg>';
    btn.addEventListener('click', function () {
      setDrawer(html.getAttribute('data-lg-drawer') !== 'open');
    });

    bar.insertBefore(btn, bar.firstChild);
    return true;
  }

  /* 手机上点一个导航项就该关抽屉：留着浮层只会挡住刚打开的那一页。 */
  document.addEventListener('click', function (e) {
    if (!isDrawer() || !e.target || !e.target.closest) return;
    if (e.target.closest('.nav-item')) setDrawer(false);
  }, true);

  function syncMode() {
    var drawer = isDrawer();
    html.setAttribute('data-lg-mode', drawer ? 'drawer' : 'wide');
    if (!drawer) setDrawer(false);
  }

  /* ---------------- 2. 桌面专属项 ---------------- */

  var HIDE_TITLES = ['开机自启', '桌面通知'];
  var HIDE_PREFIX = ['有新版本', '检查更新'];

  function trimDesktopCards() {
    var cards = document.querySelectorAll('.card');
    for (var i = cards.length - 1; i >= 0; i--) {
      var t = cards[i].querySelector('.card-title');
      if (!t) continue;
      var text = (t.textContent || '').trim();
      if (HIDE_TITLES.indexOf(text) >= 0) { cards[i].remove(); continue; }
      for (var k = 0; k < HIDE_PREFIX.length; k++) {
        if (text.indexOf(HIDE_PREFIX[k]) === 0) { cards[i].remove(); break; }
      }
    }
  }

  /* 页面每次切面/重载都会重建整棵卡片树，所以在内容区挂一个观察者。 */
  function watchCards() {
    var content = document.querySelector('.content');
    if (!content || content.__lgTrimmed) return false;
    content.__lgTrimmed = true;
    trimDesktopCards();
    new MutationObserver(trimDesktopCards).observe(content, { childList: true, subtree: true });
    return true;
  }

  var bootTries = 0;
  function afterShell() {
    if (mountMenu()) {
      watchCards();
      return;
    }
    if (bootTries++ > 80) return; /* ~5s 之后放弃：页面本来就没起来，交给它自己报错 */
    setTimeout(afterShell, 60);
  }

  function start() {
    document.body.append(scrim);
    syncMode();
    afterShell();
  }

  window.addEventListener('resize', syncMode);
  window.addEventListener('orientationchange', function () { setTimeout(syncMode, 120); });

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
})();
