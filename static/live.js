/* live.js — projector big-screen */
(function(){
  function getCode(){
    var m=location.pathname.match(/\/live\/([^\/\?#]+)/);
    if(m) return decodeURIComponent(m[1]);
    return new URLSearchParams(location.search).get('code')||'';
  }
  var code=getCode();
  function initUmamiTracking(){
    fetch('/api/settings/analytics',{credentials:'same-origin'}).then(function(r){return r.json()}).then(function(j){
      if(j && j.tracking_enabled && j.umami_script_url && j.umami_website_id){
        var s=document.createElement('script'); s.async=true; s.defer=true; s.src=j.umami_script_url; s.setAttribute('data-website-id', j.umami_website_id); document.head.appendChild(s);
      }
    }).catch(function(){});
  }
  initUmamiTracking();

  var prevFeedbackOpen=false;
  function confettiBurst(){
    var root=document.getElementById('confetti');
    if(!root) return;
    root.classList.remove('hidden'); root.textContent='';
    var colors=['#6366F1','#A855F7','#EC4899','#22D3EE','#F59E0B'];
    for(var i=0;i<22;i++){
      var el=document.createElement('i');
      el.style.left=(Math.random()*100)+'vw';
      el.style.top='-10px';
      el.style.background=colors[i%colors.length];
      el.style.animationDelay=(Math.random()*0.35)+'s';
      el.style.transform='rotate('+(Math.random()*360)+'deg)';
      root.appendChild(el);
    }
    setTimeout(function(){ root.classList.add('hidden'); root.textContent=''; }, 1700);
  }

  function renderMedia(container, q){
    if(!q || !q.media_url) return;
    var wrap=document.createElement('div'); wrap.style.marginBottom='18px';
    var el;
    if(q.media_type==='video'){
      el=document.createElement('video'); el.controls=true; el.muted=true; el.autoplay=true; el.loop=true; el.playsInline=true; el.src=q.media_url;
      el.style.width='100%'; el.style.maxHeight='420px'; el.style.borderRadius='16px';
    } else {
      el=document.createElement('img'); el.src=q.media_url; el.alt=q.prompt||'question media';
      el.style.maxWidth='100%'; el.style.maxHeight='420px'; el.style.borderRadius='16px';
    }
    wrap.appendChild(el); container.appendChild(wrap);
  }

  function renderPrompt(active){
    var area=document.getElementById('prompt-area');
    var results=document.getElementById('results-area');
    var cloud=document.getElementById('cloud-area');
    area.textContent=''; results.textContent=''; cloud.textContent='';
    var counter=document.getElementById('counter');
    if(!active){
      var w=document.createElement('div'); w.className='waiting'; w.style.minHeight='360px';
      var orb=document.createElement('div'); orb.className='orb'; orb.innerHTML='<svg width="34" height="34" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="1.7"><path d="M12 3l7 4v8l-7 4-7-4V7z"/><circle cx="12" cy="12" r="3"/></svg>';
      var h=document.createElement('h3'); h.textContent='Waiting for the host…'; h.style.fontSize='1.6rem';
      var p=document.createElement('p'); p.textContent='The host will launch the next question. Results and Q&A update live.';
      w.appendChild(orb); w.appendChild(h); w.appendChild(p); area.appendChild(w);
      if(counter) counter.textContent='— responses';
      return;
    }
    renderMedia(area, active);
    var h2=document.createElement('h2'); h2.className='live-prompt big'; h2.textContent=active.prompt; area.appendChild(h2);
    var meta=document.createElement('div'); meta.className='live-meta'; meta.style.marginTop='14px';
    var pill=document.createElement('span'); pill.className='pill live'; pill.textContent=active.kind;
    var tot=document.createElement('span'); tot.className='counter'; tot.textContent=active.show_results ? (active.total+' responses') : 'Results hidden';
    meta.appendChild(pill); meta.appendChild(tot); area.appendChild(meta);
    if(counter) counter.textContent = active.show_results ? (active.total+' responses') : 'Live';

    if(active.kind==='wordcloud'){
      renderCloud(cloud, active);
    }
    if(active.kind==='ranking'){
      renderRanking(results, active);
    } else {
      if(active.kind==='nps') renderNpsBadge(results, active);
      renderBars(results, active);
    }
  }

  function renderNpsBadge(container, active){
    if(!active.show_results) return;
    if(active.nps===null || active.nps===undefined) return;
    var b=document.createElement('div'); b.className='pill live'; b.style.cssText='margin-top:18px;font-size:1.05rem'; b.textContent='NPS '+active.nps;
    container.appendChild(b);
  }

  function renderRanking(container, active){
    if(!active.show_results) return;
    var res=active.results||[];
    if(!res.length) return;
    var max=Math.max.apply(null, res.map(function(r){return r.score||0}))||1;
    var wrap=document.createElement('div'); wrap.style.display='grid'; wrap.style.gap='12px'; wrap.style.marginTop='18px';
    res.forEach(function(r, idx){
      var row=document.createElement('div'); row.style.display='grid'; row.style.gap='6px';
      var head=document.createElement('div'); head.style.display='flex'; head.style.justifyContent='space-between'; head.style.gap='12px'; head.style.fontSize='.95rem';
      var lab=document.createElement('strong'); lab.style.letterSpacing='-.02em'; lab.textContent=(idx+1)+'. '+r.label;
      var cnt=document.createElement('span'); cnt.style.color='var(--muted)'; cnt.style.fontVariantNumeric='tabular-nums'; cnt.textContent=(r.score||0)+' pts · avg '+(r.avg_rank?r.avg_rank.toFixed(1):'0');
      head.appendChild(lab); head.appendChild(cnt);
      var track=document.createElement('div'); track.style.height='18px'; track.style.borderRadius='999px'; track.style.background='rgba(255,255,255,.07)'; track.style.overflow='hidden'; track.style.border='1px solid rgba(255,255,255,.08)';
      var fill=document.createElement('div'); fill.style.height='100%'; fill.style.borderRadius='999px'; fill.style.background='linear-gradient(135deg,#6366F1,#A855F7 45%,#EC4899 75%,#22D3EE)'; fill.style.width='0'; fill.style.transition='width .9s cubic-bezier(.16,1,.3,1)';
      track.appendChild(fill); row.appendChild(head); row.appendChild(track); wrap.appendChild(row);
      requestAnimationFrame(function(){ fill.style.width=((r.score||0)/max*100)+'%'; });
    });
    container.appendChild(wrap);
  }

  function renderBars(container, active){
    if(!active.show_results) return;
    var res=active.results||[];
    if(!res.length) return;
    var wrap=document.createElement('div'); wrap.style.display='grid'; wrap.style.gap='12px';
    var max=active.total || Math.max.apply(null, res.map(function(r){return r.count})) || 1;
    res.forEach(function(r){
      var row=document.createElement('div'); row.style.display='grid'; row.style.gap='6px';
      var head=document.createElement('div'); head.style.display='flex'; head.style.justifyContent='space-between'; head.style.gap='12px'; head.style.fontSize='.95rem';
      var lab=document.createElement('strong'); lab.style.letterSpacing='-.02em'; lab.textContent=r.label;
      var pct = active.total ? Math.round(r.count/active.total*100) : 0;
      var cnt=document.createElement('span'); cnt.style.color='var(--muted)'; cnt.style.fontVariantNumeric='tabular-nums'; cnt.textContent= r.count+' · '+pct+'%';
      head.appendChild(lab); head.appendChild(cnt);
      var track=document.createElement('div'); track.style.height='18px'; track.style.borderRadius='999px'; track.style.background='rgba(255,255,255,.07)'; track.style.overflow='hidden'; track.style.border='1px solid rgba(255,255,255,.08)';
      var fill=document.createElement('div'); fill.style.height='100%'; fill.style.borderRadius='999px'; fill.style.background='linear-gradient(135deg,#6366F1,#A855F7 45%,#EC4899 75%,#22D3EE)'; fill.style.width='0'; fill.style.transition='width .9s cubic-bezier(.16,1,.3,1)';
      track.appendChild(fill); row.appendChild(head); row.appendChild(track); wrap.appendChild(row);
      requestAnimationFrame(function(){ fill.style.width = pct+'%'; });
    });
    container.appendChild(wrap);
  }

  function renderCloud(container, active){
    if(!active.show_results) return;
    var res=(active.results||[]).filter(function(r){return r.count>0});
    var box=document.createElement('div'); box.className='cloud'; box.style.marginTop='18px'; box.style.minHeight='90px';
    if(!res.length){
      var e=document.createElement('span'); e.style.color='var(--muted)'; e.textContent='Word cloud will appear as answers come in…'; box.appendChild(e);
    } else {
      var max=Math.max.apply(null, res.map(function(r){return r.count}));
      res.sort(function(a,b){return b.count-a.count});
      res.forEach(function(r, i){
        var s=document.createElement('span'); s.className='cloud-item';
        var scale=1 + (r.count/max)*1.1;
        s.style.fontSize=(scale)+'rem';
        s.style.animationDelay=(i*30)+'ms';
        if(i===0){ s.style.background='linear-gradient(135deg,#6366F1,#EC4899)'; s.style.color='#fff'; s.style.borderColor='transparent'; }
        s.textContent=r.label;
        box.appendChild(s);
      });
    }
    container.appendChild(box);
  }

  function renderQATop(list){
    var root=document.getElementById('qa-top');
    root.textContent='';
    if(!list || !list.length){
      var empty=document.createElement('div'); empty.style.color='var(--muted)'; empty.style.fontSize='.88rem'; empty.textContent='No questions yet.';
      root.appendChild(empty); return;
    }
    var sorted=list.slice().sort(function(a,b){return b.votes-a.votes}).slice(0,10);
    sorted.forEach(function(q, idx){
      var row=document.createElement('div'); row.className='qa-rank-item';
      var num=document.createElement('div'); num.className='qa-rank-num'; num.textContent=String(idx+1);
      var body=document.createElement('div'); body.style.flex='1'; body.style.minWidth='0';
      var txt=document.createElement('div'); txt.style.fontWeight='600'; txt.style.lineHeight='1.35'; txt.style.fontSize='.92rem'; txt.textContent=q.body;
      var meta=document.createElement('div'); meta.style.color='var(--muted)'; meta.style.fontSize='.78rem'; meta.style.marginTop='4px'; meta.textContent=(q.author||'Anonymous')+' · '+q.votes+' votes';
      body.appendChild(txt); body.appendChild(meta);
      row.appendChild(num); row.appendChild(body); root.appendChild(row);
    });
  }

  function applyState(data){
    if(!data) return;
    var evt=data.event;
    if(evt){
      var nameEl=document.getElementById('event-name'); if(nameEl) nameEl.textContent=evt.name || evt.code;
      var codeEl=document.getElementById('event-code'); if(codeEl) codeEl.textContent=evt.code;
      document.title=(evt.name||'Live')+' — Projector';
      // QR
      var qrImg=document.getElementById('qr-img');
      var qrPlace=document.getElementById('qr-placeholder');
      var qrUrlEl=document.getElementById('qr-url');
      var joinUrl= location.origin + '/e/' + encodeURIComponent(evt.code);
      if(qrUrlEl) qrUrlEl.textContent=joinUrl;
      if(qrImg){
        var src='/api/events/'+encodeURIComponent(evt.code)+'/qr.png';
        if(qrImg.getAttribute('src')!==src){
          qrImg.src=src;
          qrImg.onload=function(){ qrImg.style.display='block'; if(qrPlace) qrPlace.style.display='none'; };
          qrImg.onerror=function(){ qrImg.style.display='none'; if(qrPlace) qrPlace.style.display='grid'; };
        }
      }
      // feedback open confetti
      var fbOpen = data.feedback && data.feedback.open;
      if(fbOpen && !prevFeedbackOpen) confettiBurst();
      prevFeedbackOpen = !!fbOpen;
    }
    renderPrompt(data.active_question || null);
    renderQATop(data.qa || []);
  }

  function fetchState(){
    if(!code) return;
    fetch('/api/events/'+encodeURIComponent(code)+'/state',{credentials:'same-origin'}).then(function(r){return r.json()}).then(applyState).catch(function(){});
  }
  function connect(){
    if(!code || !window.EventSource) return;
    var es=new EventSource('/api/events/'+encodeURIComponent(code)+'/stream');
    es.addEventListener('state', function(e){ try{ applyState(JSON.parse(e.data)); }catch(err){} });
  }

  if(!code){
    document.getElementById('prompt-area').textContent='No code in URL. Open /live/YOURCODE';
  } else {
    document.getElementById('event-code').textContent=code;
    fetchState(); connect();
  }
})();
