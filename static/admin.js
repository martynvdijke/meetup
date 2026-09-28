/* admin.js — vanilla admin panel */
(function(){
  function toast(msg, kind){
    kind=kind||'ok';
    var root=document.getElementById('toast-root');
    var t=document.createElement('div'); t.className='toast '+kind;
    var icon=document.createElement('span'); icon.className='toast-icon';
    icon.innerHTML = kind==='ok' ? '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>' : '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 8v5"/><path d="M12 16h.01"/></svg>';
    var txt=document.createElement('span'); txt.textContent=msg;
    t.appendChild(icon); t.appendChild(txt); root.appendChild(t);
    setTimeout(function(){ t.style.opacity='0'; t.style.transform='translateY(8px)'; t.style.transition='all .3s'; }, 2400);
    setTimeout(function(){ if(t.parentNode) t.parentNode.removeChild(t); }, 2800);
  }
  function initUmamiTracking(){
    fetch('/api/settings/analytics',{credentials:'same-origin'}).then(function(r){return r.json()}).then(function(j){
      if(j && j.tracking_enabled && j.umami_script_url && j.umami_website_id){
        var s=document.createElement('script'); s.async=true; s.defer=true; s.src=j.umami_script_url; s.setAttribute('data-website-id', j.umami_website_id); document.head.appendChild(s);
      }
    }).catch(function(){});
  }
  initUmamiTracking();

  function api(path, opts){
    opts=opts||{}; opts.credentials='same-origin';
    opts.headers = opts.headers || {};
    if(opts.body && !(opts.body instanceof FormData) && !opts.headers['Content-Type']) opts.headers['Content-Type']='application/json';
    return fetch(path, opts).then(function(r){
      if(r.status===401) throw Object.assign(new Error('unauthorized'),{code:401, r:r});
      if(r.status===204) return {};
      var ct=r.headers.get('content-type')||'';
      if(ct.indexOf('application/json')!==-1) return r.json().then(function(j){ if(!r.ok) throw Object.assign(new Error(j.error||'error'),{status:r.status,j:j}); return j; });
      if(!r.ok) throw new Error('HTTP '+r.status);
      return r;
    });
  }

  var els={
    gateSetup: document.getElementById('gate-setup'),
    gateLogin: document.getElementById('gate-login'),
    app: document.getElementById('app'),
    btnLogout: document.getElementById('btn-logout'),
    meInfo: document.getElementById('me-info'),
    eventsList: document.getElementById('events-list'),
    createWrap: document.getElementById('create-event-wrap'),
    formCreate: document.getElementById('form-create-event'),
    selectedBar: document.getElementById('selected-bar'),
    selName: document.getElementById('sel-name'),
    selMeta: document.getElementById('sel-meta'),
    btnOidcSetup: document.getElementById('btn-oidc-setup'),
    btnOidcLogin: document.getElementById('btn-oidc-login'),
  };

  var state={ user:null, events:[], selectedId:null };

  function showGate(which){
    els.gateSetup.classList.toggle('hidden', which!=='setup');
    els.gateLogin.classList.toggle('hidden', which!=='login');
    els.app.classList.toggle('hidden', which!=='app');
    els.btnLogout.classList.toggle('hidden', which!=='app');
  }

  // setup & login forms
  document.getElementById('form-setup').addEventListener('submit', function(e){
    e.preventDefault();
    var u=document.getElementById('setup-user').value.trim();
    var p=document.getElementById('setup-pass').value;
    var msg=document.getElementById('setup-msg'); msg.textContent='Creating…';
    api('/api/setup',{method:'POST', body:JSON.stringify({username:u, password:p})}).then(function(){
      msg.textContent='Account created — signing in…';
      return api('/api/auth/login',{method:'POST', body:JSON.stringify({username:u, password:p})});
    }).then(function(){ return boot(); }).catch(function(err){ msg.textContent=err.message||'Setup failed'; toast(msg.textContent,'err'); });
  });
  document.getElementById('form-login').addEventListener('submit', function(e){
    e.preventDefault();
    var u=document.getElementById('login-user').value.trim();
    var p=document.getElementById('login-pass').value;
    var msg=document.getElementById('login-msg'); msg.textContent='';
    api('/api/auth/login',{method:'POST', body:JSON.stringify({username:u, password:p})}).then(function(j){
      toast('Welcome, '+(j.user?j.user.username:u));
      boot();
    }).catch(function(err){
      msg.textContent = (err.j && err.j.error) || err.message || 'Login failed';
      toast(msg.textContent,'err');
    });
  });
  els.btnLogout.addEventListener('click', function(){
    api('/api/auth/logout',{method:'POST'}).then(function(){ boot(); }).catch(function(){ boot(); });
  });

  // OIDC buttons: shown on the setup and login gates when enabled.
  function initOIDC(){
    api('/api/auth/oidc/status').then(function(j){
      var on=!!j.enabled;
      if(els.btnOidcSetup) els.btnOidcSetup.classList.toggle('hidden', !on);
      if(els.btnOidcLogin) els.btnOidcLogin.classList.toggle('hidden', !on);
    }).catch(function(){});
  }

  function oidcErrorNotice(){
    var err=new URLSearchParams(window.location.search).get('oidc_error');
    if(err==='unknown_user'){
      var msg=document.getElementById('login-msg');
      if(msg) msg.textContent='That OIDC account is not allowed to sign in. Ask an admin for access or use a local account.';
    }
  }

  function boot(){
    initOIDC(); oidcErrorNotice();
    api('/api/setup/status').then(function(j){
      if(j.needs_setup){ showGate('setup'); return Promise.reject('setup'); }
      return api('/api/auth/me');
    }).then(function(j){
      state.user=j.user; if(els.meInfo) els.meInfo.textContent=j.user.username+' · '+j.user.role;
      showGate('app'); return loadEvents();
    }).then(function(){
      loadBranding(); loadAnalytics(); loadOtel();
    }).catch(function(err){
      if(err==='setup') return;
      if(err && err.code===401){ showGate('login'); return; }
      // if fetch failed, show login
      if(err && err.message==='unauthorized') showGate('login');
    });
  }

  // nav tabs
  var navBtns=document.querySelectorAll('.admin-nav button');
  var panels=document.querySelectorAll('[data-panel]');
  function setView(name){
    navBtns.forEach(function(b){ var on=b.getAttribute('data-view')===name; b.classList.toggle('active', on); if(on) b.setAttribute('aria-current','page'); else b.removeAttribute('aria-current'); });
    panels.forEach(function(p){ p.classList.toggle('hidden', p.getAttribute('data-panel')!==name); });
    if(name==='questions' && state.selectedId) loadQuestions(state.selectedId);
    if(name==='qa' && state.selectedId) loadQA(state.selectedId);
    if(name==='slides' && state.selectedId) loadSlides(state.selectedId);
    if(name==='results' && state.selectedId) loadResults(state.selectedId);
  }
  navBtns.forEach(function(b){ b.addEventListener('click', function(){ setView(b.getAttribute('data-view')); }); });

  // events
  document.getElementById('btn-new-event').addEventListener('click', function(){ els.createWrap.classList.remove('hidden'); });
  document.getElementById('btn-cancel-create').addEventListener('click', function(){ els.createWrap.classList.add('hidden'); });
  els.formCreate.addEventListener('submit', function(e){
    e.preventDefault();
    var payload={
      name: document.getElementById('ce-name').value.trim(),
      code: document.getElementById('ce-code').value.trim(),
      description: document.getElementById('ce-desc').value.trim(),
      event_date: document.getElementById('ce-date').value.trim()
    };
    if(!payload.name) return;
    api('/api/admin/events',{method:'POST', body:JSON.stringify(payload)}).then(function(){
      toast('Event created'); els.formCreate.reset(); els.createWrap.classList.add('hidden'); loadEvents();
    }).catch(function(err){ toast(err.message||'Create failed','err'); });
  });

  function loadEvents(){
    return api('/api/admin/events').then(function(list){
      state.events=Array.isArray(list)?list:[];
      renderEvents(state.events);
      if(state.events.length && !state.selectedId) selectEvent(state.events[0].id);
      else if(state.selectedId) updateSelectedBar();
    }).catch(function(err){ if(err.code!==401) toast('Could not load events','err'); });
  }
  function renderEvents(list){
    var root=els.eventsList; root.textContent='';
    if(!list.length){
      var empty=document.createElement('div'); empty.className='glass card-pad admin-empty'; empty.textContent='No events yet — create your first one.';
      root.appendChild(empty); return;
    }
    list.forEach(function(ev){
      var card=document.createElement('div'); card.className='glass event-card';
      if(state.selectedId===ev.id) card.style.borderColor='rgba(124,107,255,.45)';
      var top=document.createElement('div'); top.style.display='flex'; top.style.justifyContent='space-between'; top.style.gap='10px'; top.style.alignItems='flex-start';
      var left=document.createElement('div');
      var h=document.createElement('h3'); h.textContent=ev.name; left.appendChild(h);
      var meta=document.createElement('div'); meta.style.color='var(--muted)'; meta.style.fontSize='.84rem';
      meta.textContent = (ev.code||'') + (ev.event_date?' · '+ev.event_date:'') + ' · '+ev.status + (ev.feedback_open?' · feedback open':'') + ' · '+ (ev.question_count||0)+' questions' + (ev.pending_qa_count?' · '+ev.pending_qa_count+' pending':'');
      left.appendChild(meta);
      var badge=document.createElement('span'); badge.className='badge '+(ev.status==='open'?'open':'closed'); badge.textContent=ev.status;
      top.appendChild(left); top.appendChild(badge);
      var actions=document.createElement('div'); actions.className='inline';
      var btnSel=document.createElement('button'); btnSel.className='btn btn-primary btn-small'; btnSel.type='button'; btnSel.textContent= state.selectedId===ev.id ? 'Selected' : 'Manage';
      btnSel.addEventListener('click', function(){ selectEvent(ev.id); setView('questions'); });
      var aLive=document.createElement('a'); aLive.className='btn btn-ghost btn-small'; aLive.href='/live/'+encodeURIComponent(ev.code); aLive.target='_blank'; aLive.textContent='Live';
      var aAud=document.createElement('a'); aAud.className='btn btn-ghost btn-small'; aAud.href='/e/'+encodeURIComponent(ev.code); aAud.target='_blank'; aAud.textContent='Audience';
      actions.appendChild(btnSel); actions.appendChild(aLive); actions.appendChild(aAud);
      card.appendChild(top); card.appendChild(actions); root.appendChild(card);
    });
  }
  function selectEvent(id){
    state.selectedId=id;
    updateSelectedBar();
    renderEvents(state.events);
    // load all panels data
    loadQuestions(id); loadQA(id); loadSlides(id); loadResults(id);
    // fill settings form
    var ev=state.events.find(function(e){return e.id===id});
    if(ev){
      document.getElementById('es-name').value=ev.name||'';
      document.getElementById('es-desc').value=ev.description||'';
      document.getElementById('es-date').value=ev.event_date||'';
      document.getElementById('es-status').value=ev.status||'open';
      document.getElementById('es-feedback').checked=!!ev.feedback_open;
    }
  }
  function updateSelectedBar(){
    var ev=state.events.find(function(e){return e.id===state.selectedId});
    if(!ev){ els.selectedBar.classList.add('hidden'); return; }
    els.selectedBar.classList.remove('hidden');
    els.selName.textContent=ev.name;
    els.selMeta.textContent=ev.code+' · '+ev.status+' · '+(ev.event_date||'no date');
    document.getElementById('sel-live').href='/live/'+encodeURIComponent(ev.code);
    document.getElementById('sel-aud').href='/e/'+encodeURIComponent(ev.code);
    document.getElementById('link-live').href='/live/'+encodeURIComponent(ev.code);
    document.getElementById('link-audience').href='/e/'+encodeURIComponent(ev.code);
  }

  document.getElementById('btn-delete-event').addEventListener('click', function(){
    if(!state.selectedId) return;
    if(!confirm('Delete this event and all its data?')) return;
    api('/api/admin/events/'+state.selectedId,{method:'DELETE'}).then(function(){ toast('Event deleted'); state.selectedId=null; els.selectedBar.classList.add('hidden'); loadEvents(); }).catch(function(err){ toast(err.message||'Delete failed','err'); });
  });
  document.getElementById('btn-export').addEventListener('click', function(){
    if(!state.selectedId) return;
    window.location.href='/api/admin/events/'+state.selectedId+'/export.csv';
  });

  // event settings
  document.getElementById('form-event-settings').addEventListener('submit', function(e){
    e.preventDefault();
    if(!state.selectedId) return toast('Select an event first','err');
    var payload={
      name: document.getElementById('es-name').value.trim(),
      description: document.getElementById('es-desc').value.trim(),
      event_date: document.getElementById('es-date').value.trim(),
      status: document.getElementById('es-status').value,
      feedback_open: document.getElementById('es-feedback').checked
    };
    api('/api/admin/events/'+state.selectedId,{method:'PATCH', body:JSON.stringify(payload)}).then(function(){ toast('Event saved'); loadEvents(); }).catch(function(err){ toast(err.message||'Save failed','err'); });
  });

  // questions
  var qKind=document.getElementById('aq-kind');
  var qOptsWrap=document.getElementById('aq-options-wrap');
  var qOptionKinds=['poll','multi','ranking'];
  function syncQuestionKind(){ qOptsWrap.style.display = qOptionKinds.indexOf(qKind.value)>=0 ? '' : 'none'; }
  qKind.addEventListener('change', syncQuestionKind);
  syncQuestionKind();

  // question media (upload or external url)
  var qMedia={url:'',type:''};
  var qMediaFile=document.getElementById('aq-media-file');
  var qMediaUrl=document.getElementById('aq-media-url');
  var qMediaPreview=document.getElementById('aq-media-preview');
  var qMediaClear=document.getElementById('aq-media-clear');
  function mediaTypeFromURL(u){ return /\.(mp4|webm|mov)(\?.*)?$/i.test(u)?'video':'image'; }
  function renderQuestionMediaPreview(){
    qMediaPreview.textContent='';
    if(!qMedia.url){ qMediaPreview.classList.add('hidden'); qMediaClear.classList.add('hidden'); return; }
    var el;
    if(qMedia.type==='video'){ el=document.createElement('video'); el.controls=true; el.muted=true; el.src=qMedia.url; el.style.maxHeight='160px'; el.style.borderRadius='10px'; }
    else { el=document.createElement('img'); el.src=qMedia.url; el.alt='media preview'; el.style.maxHeight='160px'; el.style.borderRadius='10px'; }
    qMediaPreview.appendChild(el);
    qMediaPreview.classList.remove('hidden');
    qMediaClear.classList.remove('hidden');
  }
  function resetQuestionMedia(){ qMedia={url:'',type:''}; qMediaFile.value=''; qMediaUrl.value=''; renderQuestionMediaPreview(); }
  qMediaClear.addEventListener('click', resetQuestionMedia);
  qMediaUrl.addEventListener('change', function(){
    var u=qMediaUrl.value.trim();
    if(!u){ qMedia={url:'',type:''}; renderQuestionMediaPreview(); return; }
    qMedia={url:u,type:mediaTypeFromURL(u)}; qMediaFile.value='';
    renderQuestionMediaPreview();
  });
  qMediaFile.addEventListener('change', function(){
    var f=qMediaFile.files[0]; if(!f) return;
    if(!state.selectedId){ toast('Select an event first','err'); qMediaFile.value=''; return; }
    var fd=new FormData(); fd.append('file', f);
    toast('Uploading '+f.name+'…');
    api('/api/admin/events/'+state.selectedId+'/questions/media',{method:'POST', body:fd}).then(function(data){
      qMedia={url:data.url,type:data.media_type};
      qMediaUrl.value='';
      renderQuestionMediaPreview();
      toast('Media uploaded');
    }).catch(function(err){ toast(err.message||'Upload failed','err'); qMediaFile.value=''; });
  });

  document.getElementById('form-add-question').addEventListener('submit', function(e){
    e.preventDefault();
    if(!state.selectedId) return toast('Select an event first','err');
    var kind=document.getElementById('aq-kind').value;
    var mode=document.getElementById('aq-mode').value;
    var prompt=document.getElementById('aq-prompt').value.trim();
    var optsRaw=document.getElementById('aq-options').value.trim();
    var opts = qOptionKinds.indexOf(kind)>=0 ? optsRaw.split(',').map(function(s){return s.trim()}).filter(Boolean) : [];
    if(!prompt) return;
    if(qOptionKinds.indexOf(kind)>=0 && opts.length<2) return toast('Add at least 2 options','err');
    var payload={ kind:kind, mode:mode, prompt:prompt, options:opts, is_feedback: document.getElementById('aq-feedback').checked, show_results: document.getElementById('aq-show').checked, position: 0, media_url: qMedia.url, media_type: qMedia.type };
    api('/api/admin/events/'+state.selectedId+'/questions',{method:'POST', body:JSON.stringify(payload)}).then(function(){ toast('Question added'); document.getElementById('form-add-question').reset(); document.getElementById('aq-show').checked=true; resetQuestionMedia(); loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Add failed','err'); });
  });

  function loadQuestions(id){
    api('/api/admin/events/'+id+'/questions').then(function(list){
      renderQuestions(Array.isArray(list)?list:[]);
      document.getElementById('q-count').textContent=(Array.isArray(list)?list.length:0)+' total';
    }).catch(function(){ document.getElementById('questions-list').textContent='Could not load questions.'; });
  }
  function renderQuestions(list){
    var root=document.getElementById('questions-list'); root.textContent='';
    if(!list.length){ var e=document.createElement('div'); e.className='glass card-pad'; e.style.color='var(--muted)'; e.textContent='No questions yet.'; root.appendChild(e); return; }
    list.forEach(function(q, idx){
      var row=document.createElement('div'); row.className='q-row '+q.status;
      var top=document.createElement('div'); top.style.display='flex'; top.style.justifyContent='space-between'; top.style.gap='10px'; top.style.alignItems='flex-start';
      var left=document.createElement('div'); left.style.flex='1';
      var prompt=document.createElement('div'); prompt.style.fontWeight='700'; prompt.style.letterSpacing='-.02em'; prompt.textContent=q.prompt;
      var meta=document.createElement('div'); meta.style.color='var(--muted)'; meta.style.fontSize='.82rem'; meta.style.marginTop='4px';
      meta.textContent = q.kind+' · '+q.mode + (q.is_feedback?' · feedback':'') + ' · '+q.status + (q.show_results?' · results visible':' · results hidden') + (q.options && q.options.length ? ' · '+q.options.join(', '):'') + (q.respondents? ' · '+q.respondents+' respondents':'') + (q.media_url? ' · '+q.media_type:'');
      left.appendChild(prompt); left.appendChild(meta);
      var badge=document.createElement('span'); badge.className='badge '+(q.status==='live'?'open':''); badge.textContent=q.status;
      top.appendChild(left); top.appendChild(badge);
      var actions=document.createElement('div'); actions.className='inline'; actions.style.marginTop='6px';
      if(q.status!=='live'){
        var bAct=document.createElement('button'); bAct.className='btn btn-primary btn-small'; bAct.type='button'; bAct.textContent='Activate';
        bAct.addEventListener('click', function(){ api('/api/admin/events/'+state.selectedId+'/questions/'+q.id+'/activate',{method:'POST'}).then(function(){ toast('Activated'); loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Activate failed','err'); }); });
        actions.appendChild(bAct);
      }
      if(q.status==='live'){
        var bClose=document.createElement('button'); bClose.className='btn btn-ghost btn-small'; bClose.type='button'; bClose.textContent='Close';
        bClose.addEventListener('click', function(){ api('/api/admin/events/'+state.selectedId+'/questions/'+q.id+'/close',{method:'POST'}).then(function(){ toast('Closed'); loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Close failed','err'); }); });
        actions.appendChild(bClose);
      }
      var bDel=document.createElement('button'); bDel.className='btn btn-ghost btn-small'; bDel.type='button'; bDel.textContent='Delete'; bDel.style.color='#F87171';
      bDel.addEventListener('click', function(){ if(!confirm('Delete question?')) return; api('/api/admin/events/'+state.selectedId+'/questions/'+q.id,{method:'DELETE'}).then(function(){ toast('Deleted'); loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Delete failed','err'); }); });
      var bUp=document.createElement('button'); bUp.className='btn btn-ghost btn-small'; bUp.type='button'; bUp.textContent='↑'; bUp.title='Move up'; bUp.setAttribute('aria-label','Move up');
      bUp.addEventListener('click', function(){ moveQuestion(q, -1, list); });
      var bDown=document.createElement('button'); bDown.className='btn btn-ghost btn-small'; bDown.type='button'; bDown.textContent='↓'; bDown.title='Move down'; bDown.setAttribute('aria-label','Move down');
      bDown.addEventListener('click', function(){ moveQuestion(q, 1, list); });
      var bToggle=document.createElement('button'); bToggle.className='btn btn-ghost btn-small'; bToggle.type='button'; bToggle.textContent= q.show_results? 'Hide results':'Show results';
      bToggle.addEventListener('click', function(){ api('/api/admin/events/'+state.selectedId+'/questions/'+q.id,{method:'PATCH', body:JSON.stringify({show_results:!q.show_results})}).then(function(){ loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Update failed','err'); }); });
      actions.appendChild(bToggle); actions.appendChild(bUp); actions.appendChild(bDown); actions.appendChild(bDel);

      // inline edit prompt? small
      var editWrap=document.createElement('div'); editWrap.style.display='flex'; editWrap.style.gap='8px'; editWrap.style.marginTop='8px';
      var inp=document.createElement('input'); inp.className='input'; inp.value=q.prompt; inp.style.flex='1'; inp.setAttribute('aria-label','Edit prompt');
      var bSave=document.createElement('button'); bSave.className='btn btn-ghost btn-small'; bSave.type='button'; bSave.textContent='Save';
      bSave.addEventListener('click', function(){ api('/api/admin/events/'+state.selectedId+'/questions/'+q.id,{method:'PATCH', body:JSON.stringify({prompt:inp.value.trim()})}).then(function(){ toast('Saved'); loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Save failed','err'); }); });
      editWrap.appendChild(inp); editWrap.appendChild(bSave);

      // results mini bars
      var barWrap=document.createElement('div'); barWrap.style.display='grid'; barWrap.style.gap='6px'; barWrap.style.marginTop='8px';
      if(q.kind==='nps' && typeof q.nps==='number'){
        var npsRow=document.createElement('div'); npsRow.style.fontWeight='700'; npsRow.style.fontSize='.9rem'; npsRow.textContent='NPS '+q.nps;
        barWrap.appendChild(npsRow);
      }
      if(q.results && q.results.length){
        var total=q.total||0;
        q.results.forEach(function(r){
          var row2=document.createElement('div'); row2.style.display='flex'; row2.style.justifyContent='space-between'; row2.style.fontSize='.84rem'; row2.style.gap='10px';
          var lab=document.createElement('span'); lab.textContent=r.label; lab.style.fontWeight='600';
          var cnt=document.createElement('span'); cnt.style.color='var(--muted)';
          if(q.kind==='ranking' && typeof r.score==='number'){
            cnt.textContent=r.count+' ballots · '+r.score+' pts · avg '+(r.avg_rank||0).toFixed(2);
          }else{
            cnt.textContent=r.count + (total? ' · '+Math.round(r.count/total*100)+'%':'');
          }
          row2.appendChild(lab); row2.appendChild(cnt);
          var track=document.createElement('div'); track.style.height='8px'; track.style.borderRadius='999px'; track.style.background='rgba(255,255,255,.08)'; track.style.overflow='hidden';
          var pct = 0;
          if(q.kind==='ranking' && q.results.length){
            var maxScore=Math.max.apply(null, q.results.map(function(x){return x.score||0}));
            pct = maxScore>0 ? (r.score||0)/maxScore*100 : 0;
          }else{
            pct = total? r.count/total*100 : 0;
          }
          var fill=document.createElement('div'); fill.style.height='100%'; fill.style.background='linear-gradient(135deg,#6366F1,#EC4899)'; fill.style.width= pct+'%';
          track.appendChild(fill); barWrap.appendChild(row2); barWrap.appendChild(track);
        });
      }

      row.appendChild(top); row.appendChild(actions); row.appendChild(editWrap); if(barWrap.childNodes.length) row.appendChild(barWrap);
      root.appendChild(row);
    });
  }
  function moveQuestion(q, dir, list){
    // find index
    var idx=list.indexOf(q);
    var newIdx=idx+dir;
    if(newIdx<0 || newIdx>=list.length) return;
    var other=list[newIdx];
    // swap positions via PATCH; assume position field
    var p1=q.position, p2=other.position;
    // if equal, use idx
    if(p1===p2){ p1=idx; p2=newIdx; }
    Promise.all([
      api('/api/admin/events/'+state.selectedId+'/questions/'+q.id,{method:'PATCH', body:JSON.stringify({position:p2})}),
      api('/api/admin/events/'+state.selectedId+'/questions/'+other.id,{method:'PATCH', body:JSON.stringify({position:p1})})
    ]).then(function(){ loadQuestions(state.selectedId); }).catch(function(err){ toast(err.message||'Move failed','err'); });
  }

  // QA
  document.getElementById('btn-refresh-qa').addEventListener('click', function(){ if(state.selectedId) loadQA(state.selectedId); });
  function loadQA(id){
    api('/api/admin/events/'+id+'/qa').then(function(list){ renderQA(Array.isArray(list)?list:[]); }).catch(function(){ document.getElementById('qa-list').textContent='Could not load Q&A.'; });
  }
  function renderQA(list){
    var root=document.getElementById('qa-list'); root.textContent='';
    if(!list.length){ var e=document.createElement('div'); e.className='glass card-pad'; e.style.color='var(--muted)'; e.textContent='No questions.'; root.appendChild(e); return; }
    list.forEach(function(q){
      var card=document.createElement('div'); card.className='glass qa-item';
      var body=document.createElement('div'); body.className='qa-body'; body.textContent=q.body;
      var meta=document.createElement('div'); meta.className='qa-meta';
      var author=document.createElement('strong'); author.textContent=q.author||'Anonymous';
      var votes=document.createElement('span'); votes.textContent='· '+q.votes+' votes';
      var badge=document.createElement('span'); badge.className='pill'; badge.textContent=q.status;
      meta.appendChild(author); meta.appendChild(votes); meta.appendChild(badge);
      var actions=document.createElement('div'); actions.className='inline'; actions.style.marginTop='6px';
      ['approved','hidden','answered'].forEach(function(s){
        var b=document.createElement('button'); b.className='btn btn-ghost btn-small'; b.type='button'; b.textContent=s;
        if(q.status===s) b.style.background='rgba(124,107,255,.2)';
        b.addEventListener('click', function(){ api('/api/admin/events/'+state.selectedId+'/qa/'+q.id,{method:'PATCH', body:JSON.stringify({status:s})}).then(function(){ toast('Updated to '+s); loadQA(state.selectedId); }).catch(function(err){ toast(err.message||'Update failed','err'); }); });
        actions.appendChild(b);
      });
      var bDel=document.createElement('button'); bDel.className='btn btn-ghost btn-small'; bDel.type='button'; bDel.textContent='Delete'; bDel.style.color='#F87171';
      bDel.addEventListener('click', function(){ if(!confirm('Delete?')) return; api('/api/admin/events/'+state.selectedId+'/qa/'+q.id,{method:'DELETE'}).then(function(){ toast('Deleted'); loadQA(state.selectedId); }).catch(function(err){ toast(err.message||'Delete failed','err'); }); });
      actions.appendChild(bDel);
      card.appendChild(body); card.appendChild(meta); card.appendChild(actions); root.appendChild(card);
    });
  }

  // slides
  document.getElementById('form-upload').addEventListener('submit', function(e){
    e.preventDefault();
    if(!state.selectedId) return toast('Select an event','err');
    var title=document.getElementById('up-title').value.trim();
    var speaker=document.getElementById('up-speaker').value.trim();
    var file=document.getElementById('up-file').files[0];
    if(!file) return;
    var fd=new FormData(); fd.append('title', title); fd.append('speaker', speaker); fd.append('file', file);
    var prog=document.getElementById('up-progress'); prog.textContent='Uploading…';
    var xhr=new XMLHttpRequest();
    xhr.open('POST','/api/admin/events/'+state.selectedId+'/presentations', true);
    xhr.withCredentials=true;
    xhr.upload.onprogress=function(ev){ if(ev.lengthComputable) prog.textContent=Math.round(ev.loaded/ev.total*100)+'%'; };
    xhr.onload=function(){
      if(xhr.status>=200 && xhr.status<300){ prog.textContent='Done'; toast('Uploaded'); document.getElementById('form-upload').reset(); loadSlides(state.selectedId); setTimeout(function(){ prog.textContent=''; },1200); }
      else { prog.textContent='Failed'; toast('Upload failed','err'); }
    };
    xhr.onerror=function(){ prog.textContent='Failed'; toast('Upload failed','err'); };
    xhr.send(fd);
  });
  function loadSlides(id){
    api('/api/admin/events/'+id+'/presentations').then(function(list){ renderSlides(Array.isArray(list)?list:[]); }).catch(function(){});
  }
  function renderSlides(list){
    var root=document.getElementById('slides-list'); root.textContent='';
    if(!list.length){ var e=document.createElement('div'); e.className='glass card-pad'; e.style.color='var(--muted)'; e.textContent='No presentations yet.'; root.appendChild(e); return; }
    list.forEach(function(p){
      var row=document.createElement('div'); row.className='glass card-pad'; row.style.display='flex'; row.style.gap='12px'; row.style.alignItems='center';
      var info=document.createElement('div'); info.style.flex='1';
      var t=document.createElement('div'); t.style.fontWeight='700'; t.textContent=p.title||p.filename;
      var sub=document.createElement('div'); sub.style.color='var(--muted)'; sub.style.fontSize='.82rem'; sub.textContent=(p.speaker? p.speaker+' · ':'')+ (p.filename||'') + (p.size? ' · '+Math.round(p.size/1024)+' KB':'');
      info.appendChild(t); info.appendChild(sub);
      var a=document.createElement('a'); a.className='btn btn-ghost btn-small'; a.href=p.url; a.textContent='Open'; a.target='_blank';
      var bDel=document.createElement('button'); bDel.className='btn btn-ghost btn-small'; bDel.type='button'; bDel.textContent='Delete'; bDel.style.color='#F87171';
      bDel.addEventListener('click', function(){ if(!confirm('Delete presentation?')) return; api('/api/admin/events/'+state.selectedId+'/presentations/'+p.id,{method:'DELETE'}).then(function(){ toast('Deleted'); loadSlides(state.selectedId); }).catch(function(err){ toast(err.message||'Delete failed','err'); }); });
      row.appendChild(info); row.appendChild(a); row.appendChild(bDel); root.appendChild(row);
    });
  }

  // results
  document.getElementById('btn-refresh-results').addEventListener('click', function(){ if(state.selectedId) loadResults(state.selectedId); });
  function loadResults(id){
    api('/api/admin/events/'+id+'/questions').then(function(list){
      var root=document.getElementById('results-list'); root.textContent='';
      var qs=Array.isArray(list)?list:[];
      if(!qs.length){ root.textContent='No questions.'; return; }
      qs.forEach(function(q){
        var card=document.createElement('div'); card.className='glass card-pad';
        var title=document.createElement('div'); title.style.fontWeight='700'; title.textContent=q.prompt;
        var meta=document.createElement('div'); meta.style.color='var(--muted)'; meta.style.fontSize='.82rem'; meta.textContent=q.kind+' · '+q.status+' · '+(q.total||0)+' responses'+(q.is_feedback?' · feedback':'')+(q.media_url?' · '+q.media_type:'');
        card.appendChild(title); card.appendChild(meta);
        if(q.media_url){
          var m=document.createElement(q.media_type==='video'?'video':'img');
          if(q.media_type==='video'){ m.controls=true; m.muted=true; m.src=q.media_url; } else { m.src=q.media_url; m.alt=q.prompt||'media'; }
          m.style.cssText='max-width:100%;max-height:200px;border-radius:12px;margin-top:10px';
          card.appendChild(m);
        }
        if(q.kind==='wordcloud' && q.results){
          var cloud=document.createElement('div'); cloud.className='cloud'; cloud.style.marginTop='12px';
          var filtered=q.results.filter(function(r){return r.count>0});
          if(!filtered.length) cloud.textContent='No answers yet.';
          else {
            var max=Math.max.apply(null, filtered.map(function(r){return r.count}));
            filtered.forEach(function(r,i){
              var s=document.createElement('span'); s.className='cloud-item'; s.textContent=r.label;
              var sc=0.9 + (r.count/max)*0.6; s.style.fontSize=sc+'rem';
              if(i===0){ s.style.background='linear-gradient(135deg,#6366F1,#EC4899)'; s.style.color='#fff'; s.style.borderColor='transparent'; }
              cloud.appendChild(s);
            });
          }
          card.appendChild(cloud);
        }
        if(q.results && q.results.length){
          var wrap=document.createElement('div'); wrap.style.display='grid'; wrap.style.gap='8px'; wrap.style.marginTop='12px';
          if(q.kind==='nps' && typeof q.nps==='number'){
            var nps=document.createElement('div'); nps.style.fontWeight='700'; nps.style.marginTop='10px'; nps.style.fontSize='1rem'; nps.textContent='NPS score: '+q.nps;
            card.appendChild(nps);
          }
          var maxScore=0;
          if(q.kind==='ranking'){
            maxScore=Math.max.apply(null, q.results.map(function(x){return x.score||0}));
          }
          q.results.forEach(function(r){
            var row=document.createElement('div'); row.style.display='grid'; row.style.gap='4px';
            var head=document.createElement('div'); head.style.display='flex'; head.style.justifyContent='space-between'; head.style.fontSize='.88rem';
            var lab=document.createElement('strong'); lab.textContent=r.label;
            var cnt=document.createElement('span'); cnt.style.color='var(--muted)';
            if(q.kind==='ranking' && typeof r.score==='number'){
              cnt.textContent=r.count+' ballots · '+r.score+' pts · avg '+(r.avg_rank||0).toFixed(2);
            }else{
              cnt.textContent=r.count + (q.total? ' · '+Math.round(r.count/q.total*100)+'%':'');
            }
            head.appendChild(lab); head.appendChild(cnt);
            var track=document.createElement('div'); track.style.height='10px'; track.style.borderRadius='999px'; track.style.background='rgba(255,255,255,.07)'; track.style.overflow='hidden';
            var pct=0;
            if(q.kind==='ranking'){
              pct = maxScore>0? (r.score||0)/maxScore*100 : 0;
            }else{
              pct = q.total? (r.count/q.total*100) : 0;
            }
            var fill=document.createElement('div'); fill.style.height='100%'; fill.style.background='linear-gradient(135deg,#6366F1,#EC4899)'; fill.style.width= pct+'%';
            track.appendChild(fill); row.appendChild(head); row.appendChild(track); wrap.appendChild(row);
          });
          card.appendChild(wrap);
        }
        root.appendChild(card);
      });
      // feedback summary: filter is_feedback
      var fb=qs.filter(function(q){return q.is_feedback});
      var sum=document.getElementById('feedback-summary'); sum.textContent='';
      if(!fb.length) sum.textContent='No feedback questions configured.';
      else {
        fb.forEach(function(q){
          var d=document.createElement('div'); d.style.padding='8px 0'; d.style.borderTop='1px solid rgba(255,255,255,.06)';
          var t=document.createElement('div'); t.style.fontWeight='600'; t.textContent=q.prompt;
          var r=document.createElement('div'); r.style.color='var(--muted)'; r.textContent=(q.total||0)+' responses';
          d.appendChild(t); d.appendChild(r); sum.appendChild(d);
        });
      }
    });
  }

  // branding / analytics / otel
  function loadBranding(){
    api('/api/admin/settings/branding').then(function(j){ document.getElementById('br-brand').value=j.brand||''; }).catch(function(){});
  }
  document.getElementById('form-branding').addEventListener('submit', function(e){
    e.preventDefault();
    var brand=document.getElementById('br-brand').value.trim();
    api('/api/admin/settings/branding',{method:'PUT', body:JSON.stringify({brand:brand})}).then(function(){ toast('Branding saved'); }).catch(function(err){ toast(err.message||'Failed','err'); });
  });
  function loadAnalytics(){
    api('/api/admin/settings/analytics').then(function(j){
      document.getElementById('an-url').value=j.umami_script_url||'';
      document.getElementById('an-id').value=j.umami_website_id||'';
      document.getElementById('an-enabled').checked=!!j.tracking_enabled;
    }).catch(function(){});
  }
  document.getElementById('form-analytics').addEventListener('submit', function(e){
    e.preventDefault();
    var payload={ umami_script_url: document.getElementById('an-url').value.trim(), umami_website_id: document.getElementById('an-id').value.trim(), tracking_enabled: document.getElementById('an-enabled').checked };
    api('/api/admin/settings/analytics',{method:'PUT', body:JSON.stringify(payload)}).then(function(){ toast('Analytics saved'); }).catch(function(err){ toast(err.message||'Failed','err'); });
  });
  function renderOtelStatus(j){
    var el=document.getElementById('otel-status');
    if(!el) return;
    el.textContent=''; // clear
    var eff=j.effective||{}, src=j.source||{};
    var rows=[
      ['enabled', String(!!j.enabled)],
      ['endpoint', eff.endpoint||'—'],
      ['service name', eff.service_name||'—'],
      ['source', 'endpoint: '+(src.endpoint||'default')+' · name: '+(src.service_name||'default')+' · headers: '+(src.headers||'default')],
      ['restart required', j.restart_required?'yes':'no']
    ];
    rows.forEach(function(r){
      var div=document.createElement('div'); div.style.display='flex'; div.style.justifyContent='space-between'; div.style.gap='10px';
      var k=document.createElement('strong'); k.textContent=r[0];
      var v=document.createElement('span'); v.textContent=r[1]; v.style.color='#D6DAEA';
      div.appendChild(k); div.appendChild(v); el.appendChild(div);
    });
    var note=document.getElementById('otel-restart');
    if(note) note.classList.toggle('hidden', !j.restart_required);
  }
  function loadOtel(){
    api('/api/admin/settings/otel').then(function(j){
      var stored=j.stored||{};
      document.getElementById('ot-endpoint').value=stored.endpoint||'';
      document.getElementById('ot-service').value=stored.service_name||'';
      document.getElementById('ot-headers').value=stored.headers||'';
      renderOtelStatus(j);
      var mini=document.getElementById('otel-mini');
      if(mini) mini.textContent='OTel: '+(j.enabled?'enabled':'disabled') + (j.endpoint? ' · '+j.endpoint:'');
    }).catch(function(){ var el=document.getElementById('otel-status'); if(el) el.textContent='Could not load OTel status.'; });
  }
  var otelForm=document.getElementById('form-otel');
  if(otelForm) otelForm.addEventListener('submit', function(e){
    e.preventDefault();
    var payload={
      endpoint: document.getElementById('ot-endpoint').value.trim(),
      service_name: document.getElementById('ot-service').value.trim(),
      headers: document.getElementById('ot-headers').value.trim()
    };
    api('/api/admin/settings/otel',{method:'PUT', body:JSON.stringify(payload)}).then(function(j){
      renderOtelStatus(j);
      toast(j.restart_required?'OTel settings saved — restart the server to apply':'OTel settings saved');
    }).catch(function(err){ toast(err.message||'Failed','err'); });
  });

  // initial boot
  boot();
})();
