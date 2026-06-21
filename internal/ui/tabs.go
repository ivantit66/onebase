package ui

import "net/http"

// Оболочка вкладок рантайма (issue #129 переключение форм / #130 несколько
// экземпляров), фаза 1.
//
// Идея: вместо навигации «одна форма на страницу» — одно окно с внутренними
// вкладками. Оболочка /ui/app переиспользует head+nav (топбар, подсистемы,
// боковое меню объектов), а на месте <main> держит полосу вкладок и стек
// <iframe>. Каждая открытая форма/список/отчёт — отдельная вкладка; страницы
// внутри фреймов рендерятся как есть, но прячут свой хром (см. window.__obEmbedded
// в партиале head). Несколько экземпляров одного объекта = несколько вкладок
// (изоляция фреймов) — это закрывает #130.
//
// Прямые страницы /ui/... продолжают работать без изменений: вся embedded-логика
// активна только во фрейме оболочки.

// appShell отдаёт страницу-оболочку. Данные навигации (Nav/Subsystems/…)
// заполняет s.render как для обычных страниц.
func (s *Server) appShell(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "page-app-shell", nil)
}

const tplAppShell = `{{define "page-app-shell"}}
{{template "head" .}}{{template "nav" .}}
<style>
.ob-shell-main{flex:1;display:flex;flex-direction:column;min-width:0;overflow:hidden;padding:0}
.ob-tabstrip{display:flex;gap:2px;background:#e2e8f0;padding:4px 6px 0;overflow-x:auto;flex-shrink:0;min-height:34px}
.ob-tab{display:inline-flex;align-items:center;gap:6px;background:#f1f5f9;border:1px solid #cbd5e1;border-bottom:none;border-radius:6px 6px 0 0;padding:5px 8px 5px 12px;font-size:12px;color:#334155;cursor:pointer;white-space:nowrap;max-width:230px}
.ob-tab.active{background:#fff;color:#1a4a80;font-weight:600}
.ob-tab-label{overflow:hidden;text-overflow:ellipsis}
.ob-tab-dup{color:#94a3b8;font-size:12px;line-height:1;border-radius:3px;padding:0 3px}
.ob-tab-dup:hover{color:#1a4a80;background:#dbeafe}
.ob-tab-close{color:#94a3b8;font-size:14px;line-height:1;border-radius:3px;padding:0 3px}
.ob-tab-close:hover{color:#dc2626;background:#fee2e2}
.ob-tabbody{flex:1;position:relative;background:#fff;min-height:0}
.ob-tabbody iframe{position:absolute;inset:0;width:100%;height:100%;border:0;display:none}
.ob-tabbody iframe.active{display:block}
.ob-tabempty{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;color:#94a3b8;font-size:14px;padding:20px;text-align:center}
</style>
<main class="ob-shell-main" id="ob-shell-main">
  <div class="ob-tabstrip" id="ob-tabstrip" role="tablist"></div>
  <div class="ob-tabbody" id="ob-tabbody">
    <div class="ob-tabempty" id="ob-tabempty">{{t $.Lang "Откройте объект из меню слева"}}</div>
  </div>
</main>
<script>
(function(){
  var strip=document.getElementById('ob-tabstrip');
  var body=document.getElementById('ob-tabbody');
  var empty=document.getElementById('ob-tabempty');
  var tabs=[]; var active=null; var STORE='obTabs';

  function persist(){ try{ sessionStorage.setItem(STORE, JSON.stringify(tabs.map(function(t){return {url:t.url,title:t.title};}))); }catch(e){} }
  function syncEmpty(){ if(empty) empty.style.display = tabs.length ? 'none' : ''; }
  function setActive(t){
    active=t||null;
    tabs.forEach(function(x){ x.btn.classList.toggle('active',x===t); x.frame.classList.toggle('active',x===t); });
    syncEmpty();
  }
  function closeTab(t){
    var i=tabs.indexOf(t); if(i<0)return;
    t.btn.remove(); t.frame.remove(); tabs.splice(i,1);
    if(active===t) setActive(tabs[Math.min(i,tabs.length-1)]||null);
    persist();
  }
  function openTab(url,title,opts){
    opts=opts||{};
    if(!opts.allowDup){ for(var i=0;i<tabs.length;i++){ if(tabs[i].url===url){ setActive(tabs[i]); return tabs[i]; } } }
    var btn=document.createElement('div'); btn.className='ob-tab'; btn.setAttribute('role','tab');
    var lab=document.createElement('span'); lab.className='ob-tab-label'; lab.textContent=title||'Форма'; btn.appendChild(lab);
    var dup=document.createElement('span'); dup.className='ob-tab-dup'; dup.textContent='⧉'; dup.title='Открыть ещё один экземпляр'; btn.appendChild(dup);
    var cl=document.createElement('span'); cl.className='ob-tab-close'; cl.textContent='✕'; cl.title='Закрыть'; btn.appendChild(cl);
    var frame=document.createElement('iframe'); frame.src=url;
    var t={url:url,title:title,btn:btn,frame:frame,label:lab};
    btn.addEventListener('click',function(e){ if(e.target===cl||e.target===dup)return; setActive(t); });
    btn.addEventListener('mousedown',function(e){ if(e.button===1){ e.preventDefault(); closeTab(t); } });
    cl.addEventListener('click',function(e){ e.stopPropagation(); closeTab(t); });
    dup.addEventListener('click',function(e){ e.stopPropagation(); openTab(t.url, t.title, {allowDup:true}); }); // #130
    strip.appendChild(btn); body.appendChild(frame); tabs.push(t); setActive(t); persist();
    return t;
  }
  window.obOpenTab=openTab;

  window.addEventListener('message',function(ev){
    var d=ev.data; if(!d||typeof d!=='object')return;
    if(d.source==='obOpenTab' && d.url){ openTab(String(d.url), d.title?String(d.title):'Форма', {allowDup:!!d.allowDup}); }
    else if(d.source==='obSetTitle' && active && d.title){ active.title=String(d.title); active.label.textContent=active.title; persist(); }
  });

  function openable(href){
    if(!/^\/ui\//.test(href))return false;
    if(/^\/ui\/(admin|about|logout|login|logo|debug)\b/.test(href))return false;
    return true;
  }
  document.addEventListener('click',function(e){
    if(e.defaultPrevented||e.button!==0||e.metaKey||e.ctrlKey||e.shiftKey||e.altKey)return;
    var a=e.target.closest?e.target.closest('#ob-nav a[href], .subsys-bar a[href]'):null;
    if(!a)return;
    var href=a.getAttribute('href')||'';
    var m=href.match(/^\/ui\/?(?:\?subsystem=([^&]*))?$/);
    if(m){ e.preventDefault(); location.href='/ui/app'+(m[1]?('?subsystem='+m[1]):''); return; }
    if(!openable(href))return;
    e.preventDefault();
    openTab(href,(a.getAttribute('title')||a.textContent||'').replace(/\s+/g,' ').trim()||'Форма');
  });

  try{
    var saved=JSON.parse(sessionStorage.getItem(STORE)||'[]');
    saved.forEach(function(s){ if(s&&s.url) openTab(String(s.url), s.title?String(s.title):'Форма'); });
    if(tabs.length) setActive(tabs[0]);
  }catch(e){}
  syncEmpty();
})();
</script>
</div></body></html>
{{end}}`
