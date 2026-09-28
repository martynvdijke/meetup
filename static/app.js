/* audience app.js — vanilla, no framework, XSS-safe */
(function(){
  function toast(msg, kind){
    kind = kind || 'ok';
    var root=document.getElementById('toast-root');
    if(!root) return;
    var t=document.createElement('div');
    t.className='toast '+kind;
    var icon=document.createElement('span');
    icon.className='toast-icon';
    icon.setAttribute('aria-hidden','true');
    icon.innerHTML = kind==='ok' ? '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>' : '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 8v5"/><path d="M12 16h.01"/></svg>';
    var txt=document.createElement('span'); txt.textContent=msg;
    t.appendChild(icon); t.appendChild(txt);
    root.appendChild(t);
    setTimeout(function(){ t.style.opacity='0'; t.style.transform='translateY(8px)'; t.style.transition='all .3s'; }, 2400);
    setTimeout(function(){ if(t.parentNode) t.parentNode.removeChild(t); }, 2800);
  }
  function esc(s){ return String(s); }
  function getCode(){
    var p=location.pathname;
    var m=p.match(/\/e\/([^\/\?#]+)/);
    if(m) return decodeURIComponent(m[1]);
    var m2=p.match(/\/live\/([^\/\?#]+)/);
    if(m2) return decodeURIComponent(m2[1]);
    var q=new URLSearchParams(location.search).get('code');
    return q || '';
  }
  var code=getCode();
  var stateCache=null;

  function initUmamiTracking(){
    fetch('/api/settings/analytics',{credentials:'same-origin'}).then(function(r){return r.json()}).then(function(j){
      if(j && j.tracking_enabled && j.umami_script_url && j.umami_website_id){
        var s=document.createElement('script'); s.async=true; s.defer=true; s.src=j.umami_script_url; s.setAttribute('data-website-id', j.umami_website_id); document.head.appendChild(s);
      }
    }).catch(function(){});
  }
  initUmamiTracking();

  // tabs
  var tabs=document.querySelectorAll('.tab');
  var panels={ live: document.getElementById('panel-live'), qa: document.getElementById('panel-qa'), slides: document.getElementById('panel-slides'), feedback: document.getElementById('panel-feedback') };
  function selectTab(name){
    tabs.forEach(function(b){
      var on=b.getAttribute('data-tab')===name;
      b.setAttribute('aria-selected', on ? 'true':'false');
      b.setAttribute('tabindex', on?'0':'-1');
    });
    Object.keys(panels).forEach(function(k){
      if(panels[k]) panels[k].classList.toggle('hidden', k!==name);
    });
    try{ localStorage.setItem('meetup_tab', name); }catch(e){}
  }
  tabs.forEach(function(b){
    b.addEventListener('click', function(){ selectTab(b.getAttribute('data-tab')); });
    b.addEventListener('keydown', function(e){
      if(e.key==='ArrowRight' || e.key==='ArrowLeft'){
        e.preventDefault();
        var arr=Array.prototype.slice.call(tabs);
        var idx=arr.indexOf(b);
        var next = e.key==='ArrowRight' ? (idx+1)%arr.length : (idx-1+arr.length)%arr.length;
        arr[next].focus(); selectTab(arr[next].getAttribute('data-tab'));
      }
    });
  });
  // restore tab if any
  try{ var saved=localStorage.getItem('meetup_tab'); if(saved && panels[saved]) selectTab(saved); }catch(e){}

  // helpers for rendering
  function renderEvent(evt){
    var n=document.getElementById('event-name');
    var c=document.getElementById('event-code');
    var d=document.getElementById('event-desc');
    if(!evt){ n.textContent='Event not found'; return; }
    n.textContent=evt.name || evt.code;
    c.textContent=evt.code;
    var brand = evt.brand ? ' · '+evt.brand : '';
    var date = evt.event_date ? ' · '+esc(evt.event_date) : '';
    var status = evt.status ? ' · '+(evt.status==='open'?'Open':'Closed') : '';
    d.textContent = (evt.description||'') + brand + date + status;
    document.title = esc(evt.name) + ' — Meetup';
  }

  function renderLive(active){
    var card=document.getElementById('live-card');
    card.textContent='';
    if(!active){
      var w=document.createElement('div'); w.className='waiting';
      var orb=document.createElement('div'); orb.className='orb';
      orb.innerHTML='<svg width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="white" stroke-width="1.8"><path d="M12 3l7 4v8l-7 4-7-4V7z"/><circle cx="12" cy="12" r="2.8"/></svg>';
      var h=document.createElement('h3'); h.textContent='Waiting for the host…';
      var p=document.createElement('p'); p.textContent='The host will start a live question soon. Stay on this tab — it updates automatically.';
      w.appendChild(orb); w.appendChild(h); w.appendChild(p);
      card.appendChild(w); return;
    }
    var prompt=document.createElement('h2'); prompt.className='prompt'; prompt.textContent=active.prompt; card.appendChild(prompt);
    var meta=document.createElement('div'); meta.style.cssText='display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin-top:8px';
    var pill=document.createElement('span'); pill.className='pill'; pill.textContent=active.kind;
    var total=document.createElement('span'); total.style.color='var(--muted)'; total.style.fontSize='.84rem'; total.textContent= active.show_results ? (active.total+' responses') : 'Responses hidden until host reveals';
    meta.appendChild(pill); meta.appendChild(total); card.appendChild(meta);

    if(active.answered){
      var th=document.createElement('div'); th.className='thanks'; th.style.marginTop='12px';
      th.innerHTML='<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 13l4 4L19 7"/></svg>';
      var s=document.createElement('span'); s.textContent='Thanks — your answer is in. You can wait for results or keep exploring Q&A.';
      th.appendChild(s); card.appendChild(th);
    }

    if(active.kind==='poll'){
      var opts=active.options||[];
      if(!active.answered){
        var list=document.createElement('div'); list.style.display='grid'; list.style.gap='10px'; list.style.marginTop='16px';
        opts.forEach(function(opt){
          var b=document.createElement('button'); b.className='option-btn'; b.type='button'; b.textContent=opt;
          b.addEventListener('click', function(){ submitAnswer(active.id, opt, b); });
          list.appendChild(b);
        });
        card.appendChild(list);
      }
      renderBars(card, active);
    } else if(active.kind==='rating'){
      if(!active.answered){
        var stars=document.createElement('div'); stars.className='stars'; stars.style.marginTop='16px'; stars.setAttribute('role','group'); stars.setAttribute('aria-label','Rating 1 to 5');
        for(var i=1;i<=5;i++){
          (function(v){
            var btn=document.createElement('button'); btn.className='star'; btn.type='button'; btn.setAttribute('aria-label','Rate '+v+' of 5'); btn.textContent=String(v);
            // use star svg overlay?
            btn.addEventListener('click', function(){ submitAnswer(active.id, String(v), btn); });
            stars.appendChild(btn);
          })(i);
        }
        card.appendChild(stars);
      }
      renderBars(card, active);
    } else if(active.kind==='open'){
      if(!active.answered){
        var form=document.createElement('form'); form.style.display='grid'; form.style.gap='10px'; form.style.marginTop='14px';
        var ta=document.createElement('textarea'); ta.className='textarea'; ta.placeholder='Type your answer…'; ta.required=true; ta.rows=3; ta.maxLength=1000;
        var btn=document.createElement('button'); btn.className='btn btn-primary'; btn.type='submit'; btn.textContent='Submit';
        form.appendChild(ta); form.appendChild(btn);
        form.addEventListener('submit', function(e){
          e.preventDefault(); if(!ta.value.trim()) return;
          submitAnswer(active.id, ta.value.trim(), btn);
        });
        card.appendChild(form);
      }
      // for open, show result bars only if show_results
      if(active.show_results) renderBars(card, active);
    } else if(active.kind==='wordcloud'){
      if(!active.answered){
        var form2=document.createElement('form'); form2.style.display='grid'; form2.style.gap='10px'; form2.style.marginTop='14px';
        var ta2=document.createElement('textarea'); ta2.className='textarea'; ta2.placeholder='One or two words…'; ta2.required=true; ta2.rows=2; ta2.maxLength=80;
        var b2=document.createElement('button'); b2.className='btn btn-primary'; b2.type='submit'; b2.textContent='Send';
        form2.appendChild(ta2); form2.appendChild(b2);
        form2.addEventListener('submit', function(e){ e.preventDefault(); if(!ta2.value.trim()) return; submitAnswer(active.id, ta2.value.trim(), b2); });
        card.appendChild(form2);
      }
      renderWordCloud(card, active);
      if(active.show_results) renderBars(card, active);
    }
  }

  function renderBars(container, active){
    if(!active.show_results) return;
    var res=active.results||[];
    if(!res.length) return;
    var max=Math.max.apply(null, res.map(function(r){return r.count})) || 1;
    // if total==0 hide counts? still show 0 bars
    var wrap=document.createElement('div'); wrap.className='results';
    res.forEach(function(r){
      var row=document.createElement('div'); row.className='result-row';
      var head=document.createElement('div'); head.className='result-head';
      var lab=document.createElement('strong'); lab.textContent=r.label;
      var cnt=document.createElement('span'); cnt.textContent= r.count + ' · ' + (active.total ? Math.round(r.count/active.total*100)+'%' : '0%');
      head.appendChild(lab); head.appendChild(cnt);
      var track=document.createElement('div'); track.className='track';
      var fill=document.createElement('div'); fill.className='fill';
      // width based on max or total
      var pct = active.total ? (r.count/active.total*100) : (r.count/max*100);
      // animate next frame
      row.appendChild(head); track.appendChild(fill); row.appendChild(track); wrap.appendChild(row);
      requestAnimationFrame(function(){ fill.style.width = pct+'%'; });
    });
    container.appendChild(wrap);
  }

  function renderWordCloud(container, active){
    if(!active.show_results) return;
    var res=active.results||[];
    if(!res.length) return;
    var cloud=document.createElement('div'); cloud.className='cloud'; cloud.style.marginTop='14px';
    // filter 0 counts
    var filtered=res.filter(function(r){return r.count>0});
    if(!filtered.length){
      var empty=document.createElement('span'); empty.style.color='var(--muted)'; empty.style.fontSize='.88rem'; empty.textContent='No answers yet — be the first!';
      cloud.appendChild(empty);
    } else {
      var max=Math.max.apply(null, filtered.map(function(r){return r.count}));
      filtered.sort(function(a,b){return b.count-a.count});
      filtered.forEach(function(r, idx){
        var s=document.createElement('span'); s.className='cloud-item';
        // scale font by count
        var scale = 0.82 + (r.count/max)*0.55;
        var opacity = 0.85 + (r.count/max)*0.15;
        s.style.fontSize = (scale)+'rem';
        s.style.opacity = String(opacity);
        s.style.animationDelay = (idx*45)+'ms';
        if(idx===0){ s.style.background='var(--grad)'; s.style.color='#fff'; s.style.borderColor='transparent'; }
        s.textContent=r.label;
        cloud.appendChild(s);
      });
    }
    container.appendChild(cloud);
  }

  function renderQA(list){
    var root=document.getElementById('qa-list');
    root.textContent='';
    if(!list || !list.length){
      var empty=document.createElement('div'); empty.className='glass card-pad'; empty.style.color='var(--muted)'; empty.textContent='No questions yet. Be the first to ask!';
      root.appendChild(empty); return;
    }
    // sort by votes desc
    var sorted=list.slice().sort(function(a,b){return b.votes-a.votes});
    sorted.forEach(function(q){
      var card=document.createElement('div'); card.className='glass qa-item';
      var body=document.createElement('div'); body.className='qa-body'; body.textContent=q.body;
      var meta=document.createElement('div'); meta.className='qa-meta';
      var author=document.createElement('strong'); author.textContent = q.author ? q.author : 'Anonymous';
      var votes=document.createElement('span'); votes.textContent='· '+q.votes+' votes';
      var btn=document.createElement('button'); btn.className='vote-btn'+(q.voted?' voted':''); btn.type='button';
      btn.innerHTML='<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><path d="M12 19l-7-7 1.4-1.4L12 16.2l5.6-5.6L19 12z"/><path d="M12 19V5"/></svg> '+(q.voted?'Voted':'Upvote');
      // different icon: arrow up
      btn.innerHTML='<svg viewBox="0 0 24 24" fill="'+(q.voted?'currentColor':'none')+'" stroke="currentColor" stroke-width="1.8"><path d="M12 5l7 7-1.4 1.4L12 7.8 6.4 13.4 5 12z"/><path d="M12 5v14"/></svg> '+(q.voted?'Voted':'Upvote');
      btn.setAttribute('aria-pressed', q.voted?'true':'false');
      btn.setAttribute('aria-label', q.voted?'Already voted':'Upvote this question');
      if(q.voted) btn.disabled=true;
      btn.addEventListener('click', function(){
        if(btn.disabled) return;
        btn.disabled=true;
        fetch('/api/events/'+encodeURIComponent(code)+'/qa/'+q.id+'/vote',{method:'POST',credentials:'same-origin'}).then(function(r){
          if(!r.ok) throw new Error('vote failed');
          return r.json();
        }).then(function(j){
          q.votes=j.votes; q.voted=true; renderQA(sorted);
          toast('Upvoted — thanks!');
        }).catch(function(){ btn.disabled=false; toast('Could not vote','err'); });
      });
      meta.appendChild(author); meta.appendChild(votes); meta.appendChild(btn);
      card.appendChild(body); card.appendChild(meta); root.appendChild(card);
    });
  }

  function renderSlides(list){
    var root=document.getElementById('slides-list');
    root.textContent='';
    if(!list || !list.length){
      var e=document.createElement('div'); e.style.color='var(--muted)'; e.style.fontSize='.9rem'; e.textContent='No slides available yet.';
      root.appendChild(e); return;
    }
    list.forEach(function(p){
      var row=document.createElement('div'); row.style.cssText='display:flex;gap:12px;align-items:center;padding:12px;border:1px solid var(--border);border-radius:12px;background:rgba(255,255,255,.04)';
      var icon=document.createElement('div'); icon.style.cssText='width:42px;height:42px;border-radius:10px;display:grid;place-items:center;background:rgba(124,107,255,.18);color:#A5B4FC;flex-shrink:0';
      icon.innerHTML='<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><path d="M14 2v6h6"/></svg>';
      var info=document.createElement('div'); info.style.flex='1'; info.style.minWidth='0';
      var t=document.createElement('div'); t.style.fontWeight='700'; t.style.letterSpacing='-.02em'; t.textContent=p.title || p.filename;
      var sub=document.createElement('div'); sub.style.color='var(--muted)'; sub.style.fontSize='.82rem'; sub.textContent=(p.speaker? p.speaker+' · ':'') + (p.size? (Math.round(p.size/1024)+' KB · ') : '') + (p.filename||'');
      info.appendChild(t); info.appendChild(sub);
      var a=document.createElement('a'); a.className='btn btn-ghost btn-small'; a.href=p.url; a.textContent='Download'; a.setAttribute('download','');
      a.setAttribute('aria-label','Download '+ (p.title||p.filename));
      row.appendChild(icon); row.appendChild(info); row.appendChild(a); root.appendChild(row);
    });
  }

  function renderFeedback(feedback, eventObj){
    var card=document.getElementById('feedback-card');
    card.textContent='';
    if(!feedback || !feedback.open){
      var title=document.createElement('h2'); title.className='section-title'; title.textContent='Feedback';
      var sub=document.createElement('p'); sub.className='section-sub'; sub.textContent='Feedback is not open yet. The host will open it during or after the event.';
      var badge=document.createElement('div'); badge.className='pill'; badge.style.marginTop='12px'; badge.textContent='Closed';
      card.appendChild(title); card.appendChild(sub); card.appendChild(badge);
      return;
    }
    var qs=feedback.questions||[];
    if(!qs.length){
      var t2=document.createElement('h2'); t2.className='section-title'; t2.textContent='Feedback'; card.appendChild(t2);
      var p2=document.createElement('p'); p2.style.color='var(--muted)'; p2.textContent='Feedback is open but no questions are configured.';
      card.appendChild(p2); return;
    }
    var h=document.createElement('h2'); h.className='section-title'; h.textContent='Feedback'; card.appendChild(h);
    var intro=document.createElement('p'); intro.className='section-sub'; intro.textContent='Your feedback helps the host improve. Thanks for taking a minute.'; card.appendChild(intro);
    var form=document.createElement('form'); form.style.display='grid'; form.style.gap='18px'; form.style.marginTop='16px';
    var states={};
    qs.forEach(function(q){
      var wrap=document.createElement('div'); wrap.style.display='grid'; wrap.style.gap='8px';
      var label=document.createElement('label'); label.style.fontWeight='650'; label.style.letterSpacing='-.02em'; label.textContent=q.prompt; label.setAttribute('for','fb-'+q.id);
      wrap.appendChild(label);
      if(q.kind==='rating'){
        var stars=document.createElement('div'); stars.className='stars'; stars.setAttribute('role','group'); stars.setAttribute('aria-label', q.prompt);
        var hidden=document.createElement('input'); hidden.type='hidden'; hidden.id='fb-'+q.id;
        for(var i=1;i<=5;i++){
          (function(v){
            var b=document.createElement('button'); b.type='button'; b.className='star'; b.textContent=String(v); b.setAttribute('aria-label', v+' of 5');
            b.addEventListener('click', function(){
              hidden.value=String(v);
              Array.prototype.forEach.call(stars.querySelectorAll('.star'), function(s, idx){ s.classList.toggle('active', idx < v); });
            });
            stars.appendChild(b);
          })(i);
        }
        wrap.appendChild(stars); wrap.appendChild(hidden);
        states[q.id]=hidden;
      } else {
        var ta=document.createElement('textarea'); ta.className='textarea'; ta.id='fb-'+q.id; ta.rows=3; ta.placeholder='Your answer…'; ta.maxLength=2000;
        wrap.appendChild(ta); states[q.id]=ta;
      }
      form.appendChild(wrap);
    });
    var submit=document.createElement('button'); submit.className='btn btn-primary'; submit.type='submit'; submit.textContent='Submit feedback';
    var hint=document.createElement('div'); hint.style.color='var(--muted)'; hint.style.fontSize='.82rem'; hint.textContent='Each answer is submitted individually.';
    form.appendChild(submit); form.appendChild(hint);
    form.addEventListener('submit', function(e){
      e.preventDefault();
      var tasks=[];
      qs.forEach(function(q){
        var el=states[q.id]; var val=(el.value||'').trim();
        if(!val) return;
        tasks.push(fetch('/api/events/'+encodeURIComponent(code)+'/answers',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({question_id:q.id, value:val})}).then(function(r){
          if(!r.ok) throw new Error('failed '+q.id);
          el.value=''; // clear
          // reset stars
          if(q.kind==='rating'){
            var container=el.previousElementSibling; if(container) Array.prototype.forEach.call(container.querySelectorAll('.star'), function(s){ s.classList.remove('active'); });
          }
        }));
      });
      if(!tasks.length){ toast('Please fill at least one field','err'); return; }
      submit.disabled=true; submit.textContent='Sending…';
      Promise.all(tasks).then(function(){ toast('Feedback sent — thank you!'); }).catch(function(){ toast('Some answers failed to send','err'); }).finally(function(){ submit.disabled=false; submit.textContent='Submit feedback'; });
    });
    card.appendChild(form);
  }

  function submitAnswer(qid, value, btn){
    var orig=btn.textContent; btn.disabled=true; btn.textContent='Sending…';
    fetch('/api/events/'+encodeURIComponent(code)+'/answers',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({question_id:qid, value:value})}).then(function(r){
      if(!r.ok) throw new Error('answer failed');
      return r.json();
    }).then(function(){
      toast('Answer sent!');
      // optimistic: keep disabled, will be updated via SSE
      btn.textContent='Sent ✓';
    }).catch(function(){ btn.disabled=false; btn.textContent=orig; toast('Could not submit answer','err'); });
  }

  // QA submit
  var qaForm=document.getElementById('qa-form');
  if(qaForm){
    qaForm.addEventListener('submit', function(e){
      e.preventDefault();
      var body=document.getElementById('qa-body').value.trim();
      var author=document.getElementById('qa-author').value.trim();
      if(!body) return;
      var btn=qaForm.querySelector('button[type="submit"]');
      btn.disabled=true; btn.textContent='Submitting…';
      fetch('/api/events/'+encodeURIComponent(code)+'/qa',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({body:body, author:author})}).then(function(r){
        if(!r.ok) throw new Error('qa failed');
        return r.json();
      }).then(function(j){
        document.getElementById('qa-body').value=''; document.getElementById('qa-author').value='';
        toast(j.status==='pending' ? 'Question submitted — awaiting approval' : 'Question posted!');
        // re-fetch qa list? will come via SSE
        fetchState();
      }).catch(function(){ toast('Could not submit question','err'); }).finally(function(){ btn.disabled=false; btn.textContent='Submit question'; });
    });
  }

  function applyState(data){
    if(!data) return;
    stateCache=data;
    if(data.event) renderEvent(data.event);
    renderLive(data.active_question || null);
    renderQA(data.qa || []);
    // slides are fetched separately but state may not include; fetch via presentations endpoint
    renderFeedback(data.feedback || {open:false, questions:[]}, data.event);
  }

  function fetchState(){
    if(!code){ renderEvent(null); return Promise.resolve(); }
    var p=fetch('/api/events/'+encodeURIComponent(code)+'/state',{credentials:'same-origin'}).then(function(r){
      if(!r.ok) throw new Error('state '+r.status);
      return r.json();
    }).then(function(j){ applyState(j); }).catch(function(err){
      var card=document.getElementById('live-card');
      card.textContent=''; var p=document.createElement('p'); p.style.color='var(--muted)'; p.textContent='Could not load event. Check the link and try again.';
      card.appendChild(p);
    });
    // also fetch presentations
    fetch('/api/events/'+encodeURIComponent(code)+'/presentations',{credentials:'same-origin'}).then(function(r){return r.json()}).then(function(j){ renderSlides(Array.isArray(j)?j:[]); }).catch(function(){});
    return p;
  }

  function connectStream(){
    if(!code || !window.EventSource) return;
    var es=new EventSource('/api/events/'+encodeURIComponent(code)+'/stream');
    es.addEventListener('state', function(e){
      try{ var data=JSON.parse(e.data); applyState(data); }catch(err){}
    });
    es.onerror=function(){
      // EventSource auto-reconnects; show subtle offline?
    };
  }

  if(!code){
    renderEvent(null);
    document.getElementById('live-card').textContent='No event code in URL. Open /e/YOURCODE';
  } else {
    document.getElementById('event-code').textContent=code;
    // set live badge code
    // open SSE only after the first state fetch so the participant cookie is
    // set before the stream binds to an identity
    fetchState().then(connectStream, connectStream);
  }
})();
