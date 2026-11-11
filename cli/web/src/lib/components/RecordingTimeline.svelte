<script lang="ts">
  import { onDestroy, tick, untrack } from 'svelte';
  import { api } from '../api';
  import { app, setPref } from '../app.svelte';
  import { route, setParams } from '../router.svelte';
  import type { RecordingCommand, RecordingGroup, RecordingShell, ReplayInfo } from '../types';
  import CopyButton from './CopyButton.svelte';
  import Highlight from './Highlight.svelte';
  import Modal from './Modal.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import '../vendor/terminal-themes.css';
  import playerURL from '../vendor/asciinema-player.min.js?url';
  import '../vendor/asciinema-player.css';

  type Player = {dispose:()=>void; seek:(n:number)=>Promise<void>; pause:()=>void; play:()=>void; getCurrentTime:()=>Promise<number>};
  type PlayerFactory = {create:(url:string, target:HTMLElement, options:Record<string,unknown>)=>Player};
  let terminalTheme = $state(app.prefs.replayTheme ?? 'terminal-app-dark');
  const palettes = ['terminal-app-dark','terminal-app-light','asciinema','dracula','monokai','nord','solarized-dark','solarized-light','tango'];
  let commands: RecordingCommand[] = $state([]), shells: RecordingShell[] = $state([]), groups: RecordingGroup[] = $state([]);
  let query = $state(''), regex = $state(false), matchCase = $state(false), archived = $state(untrack(()=>route.params.archived==='1'));
  let loading = $state(true), searching = $state(false), error = $state(''), searchError = $state(''), truncated = $state(false);
  let selected: RecordingCommand | null = $state(null), selectedSub = $state(''), selectedTime = $state(0);
  let info: ReplayInfo | null = $state(null), player: Player | null = null, playerTarget: HTMLDivElement;
  let preparing = $state(false), replayError = $state(''), replayTime = $state<number | null>(null);
  let expandedDays = $state(new Set<string>()), initializedDays = false;
  let paneWidth = $state(app.prefs.timelineWidth ?? 390), dragging = $state(false);
  let host = $state<HTMLDivElement>(null!);
  let follow = $state(false), countAtPause = $state(0), list: HTMLDivElement;
  let outputError = $state('');
  let output = $state(''), outputLoading = $state(false), showOutput = $state(false), allowLarge: RecordingShell | null = $state(null);
  let loadID = 0, searchID = 0, openID = 0, outputID = 0, disposed = false;
  const day = (ms:number) => new Date(ms).toLocaleDateString('en-CA', {timeZone:'Europe/Berlin'});
  const clock = (ms:number) => new Date(ms).toLocaleTimeString('en-GB', {timeZone:'Europe/Berlin', hour12:false});
  const days = $derived.by(() => {
    const grouped = new Map<string, RecordingCommand[]>();
    for (const c of commands) { const d=day(c.t); if(!grouped.has(d))grouped.set(d,[]);grouped.get(d)!.push(c); }
    return [...grouped];
  });
  const hitCount = $derived(groups.reduce((n,g)=>n+g.hits.length+(g.commandHit?1:0),0));
  const shell = $derived(shells.find(s=>s.sub===selectedSub));
  const newCount = $derived(Math.max(0,commands.length-countAtPause));

  function resetPlayer() { openID++; outputID++; player?.dispose(); player=null; info=null; selected=null; selectedSub=''; output=''; showOutput=false; replayTime=null; replayError=''; }
  async function refresh() {
    const id=++loadID, name=app.engagement, scope=archived;
    if (!name) { loading=false; return; }
    try {
      const data=await api.recordingTimeline(name,scope);
      if (disposed || id!==loadID || name!==app.engagement || scope!==archived) return;
      if(loading)countAtPause=data.commands.length;
      commands=data.commands; shells=data.shells; error='';
      if (!initializedDays && commands.length) { expandedDays=new Set([day(commands.at(-1)!.t)]); initializedDays=true; }
      if (follow) { countAtPause=commands.length; await tick(); if(list)list.scrollTop=list.scrollHeight; }
    } catch(e) { if(id===loadID)error=(e as Error).message; }
    finally { if(id===loadID)loading=false; }
  }
  $effect(()=>{
    const name=app.engagement, scope=archived;
    commands=[]; shells=[]; groups=[]; initializedDays=false; expandedDays=new Set(); loading=true;
    query=untrack(()=>route.params.q??''); searchError=''; countAtPause=0; follow=false; resetPlayer();
    void refresh();
    const timer=setInterval(()=>document.visibilityState==='visible'&&refresh(),4000);
    return()=>clearInterval(timer);
  });
  $effect(()=>{
    const q=query, re=regex, cs=matchCase, name=app.engagement, scope=archived;
    const id=++searchID;
    if(!q.trim()){groups=[];searchError='';searching=false;return;}
    searching=true;
    const controller=new AbortController();
    const timer=setTimeout(async()=>{
      try {
        const result=await api.recordingSearch(name,q,re,cs,scope,controller.signal);
        if(disposed||id!==searchID)return;
        groups=result.groups;truncated=result.truncated;searchError=result.error??'';
      }catch(e){if(id===searchID)searchError=(e as Error).message;}
      finally{if(id===searchID)searching=false;}
    },250);
    return()=>{clearTimeout(timer);controller.abort();};
  });
  function toggleDay(d:string){const next=new Set(expandedDays);next.has(d)?next.delete(d):next.add(d);expandedDays=next;}
  function factory():Promise<PlayerFactory>{
    const current=(window as unknown as {AsciinemaPlayer?:PlayerFactory}).AsciinemaPlayer;
    if(current)return Promise.resolve(current);
    return new Promise((resolve,reject)=>{
      const script=document.createElement('script');script.src=playerURL;
      script.onload=()=>resolve((window as unknown as {AsciinemaPlayer:PlayerFactory}).AsciinemaPlayer);
      script.onerror=()=>{script.remove();reject(new Error('Could not load the locally bundled replay player.'));};
      document.head.append(script);
    });
  }
  function castAt(data:ReplayInfo,ms:number){
    const table=data.table;if(!table.length)return 0;
    let i=0;while(i+1<table.length&&table[i+1][1]<=ms)i++;
    let t=table[i][0]+(ms-table[i][1])/1000;
    if(i+1<table.length)t=Math.min(t,table[i+1][0]);return Math.max(0,t);
  }
  function realAt(data:ReplayInfo,t:number){
    const table=data.table;if(!table.length)return null;
    let i=0;while(i+1<table.length&&table[i+1][0]<=t)i++;
    return table[i][1]+(t-table[i][0])*1000;
  }
  async function open(sub:string,time:number,command:RecordingCommand|null=null,allowed=false){
    const candidate=shells.find(s=>s.sub===sub);
    if(candidate?.big&&!allowed&&selectedSub!==sub){allowLarge=candidate;selectedTime=time;selected=command;return;}
    const id=++openID,name=app.engagement,scope=archived;
    selected=command;selectedTime=time;output='';outputError='';showOutput=false;outputID++;replayError='';preparing=true;
    try{
      const data=await api.replayInfo(name,sub,scope);
      if(disposed||id!==openID||name!==app.engagement)return;
      if(selectedSub!==sub||!player||info?.revision!==data.revision){
        const library=await factory();
        if(disposed||id!==openID||name!==app.engagement)return;
        player?.dispose();playerTarget.replaceChildren();
        info=data;selectedSub=sub;
        player=library.create(api.replayURL(name,sub,scope),playerTarget,{
          markers:data.markers,theme:terminalTheme,
          preload:true,startAt:castAt(data,time)+.001,fit:'both',terminalFontFamily:'ui-monospace, Menlo, monospace',
        });
      }
      if(id!==openID||!player||!info)return;
      await player.seek(castAt(info,time)+.001);if(id!==openID)return;
      player.pause();replayTime=time;
      await tick();playerTarget.querySelector<HTMLElement>('[tabindex]')?.focus({preventScroll:true});
    }catch(e){if(id===openID)replayError=(e as Error).message;}
    finally{if(id===openID)preparing=false;}
  }
  async function changePalette(theme:string){
    terminalTheme=theme;setPref('replayTheme',theme);
    if(!player||!info||!selectedSub)return;
    const old=player,id=++openID,position=await old.getCurrentTime();
    const library=await factory();if(id!==openID||disposed)return;
    old.dispose();playerTarget.replaceChildren();
    player=library.create(api.replayURL(app.engagement,selectedSub,archived),playerTarget,{markers:info.markers,theme,preload:true,startAt:position,fit:'both',terminalFontFamily:'ui-monospace, Menlo, monospace'});
    await player.seek(position);player.pause();
  }
  async function loadOutput(){
    showOutput=!showOutput;if(!showOutput||!selected)return;
    const id=++outputID,command=selected,name=app.engagement;outputLoading=true;outputError='';
    try{
      const recording=await api.session(name,command.sessionId??command.sub,archived);
      if(id!==outputID||selected?.id!==command.id)return;
      const entry=recording.entries?.find(e=>e.id===command.id);
      if(!entry)throw new Error('This command is no longer available. Refresh the timeline.');
      output=entry.output??'';
    }catch(e){if(id===outputID)outputError=(e as Error).message;}
    finally{if(id===outputID)outputLoading=false;}
  }
  function readPosition(){if(!list)return;if(list.scrollTop+list.clientHeight<list.scrollHeight-40&&follow){follow=false;countAtPause=commands.length;}}
  async function resume(){if(commands.length)expandedDays=new Set([...expandedDays,day(commands.at(-1)!.t)]);follow=true;countAtPause=commands.length;await tick();if(list)list.scrollTop=list.scrollHeight;}
  function resize(event:PointerEvent){(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);dragging=true;}
  function move(event:PointerEvent){if(dragging){paneWidth=Math.min(Math.max(event.clientX-host.getBoundingClientRect().left,260),Math.max(260,host.clientWidth-380));}}
  function saveWidth(){if(dragging){dragging=false;setPref('timelineWidth',paneWidth);}}
  function keyResize(e:KeyboardEvent){if(e.key==='ArrowLeft'||e.key==='ArrowRight'){e.preventDefault();paneWidth=Math.max(260,Math.min(host.clientWidth-380,paneWidth+(e.key==='ArrowRight'?24:-24)));setPref('timelineWidth',paneWidth);}}
  async function shortcuts(e:KeyboardEvent){
    if(e.ctrlKey||e.metaKey||e.altKey||(e.target as HTMLElement)?.closest('input,textarea,select,button,[role=separator]'))return;
    if(!player||!info)return;
    if(e.key==='n'||e.key==='p'){
      e.preventDefault();const t=await player.getCurrentTime(),times=info.markers.map(m=>m[0]);
      const next=e.key==='n'?times.find(x=>x>t+.01):[...times].reverse().find(x=>x<t-.01);
      if(next!==undefined){await player.seek(next);player.pause();}
    }
  }
  const timeTimer=setInterval(async()=>{if(player&&info){const p=player;try{const t=await p.getCurrentTime();if(p===player&&info)replayTime=realAt(info,t);}catch{ /* disposed player */ }}},200);
  onDestroy(()=>{disposed=true;loadID++;searchID++;openID++;outputID++;clearInterval(timeTimer);player?.dispose();});
</script>
<svelte:window onkeydown={shortcuts}/>
<div class="timeline" bind:this={host} class:dragging style={`--timeline-width:${paneWidth}px`}>
  <aside aria-label="Engagement command timeline">
    <div class="timeline-tools">
      <div class="scope"><strong>All shells</strong><span class="grow"></span><button class="sm ghost" aria-label="Refresh command timeline" onclick={refresh}>Refresh</button></div>
      <div class="scope"><button class="sm" aria-pressed={!archived} onclick={()=>archived=false}>Current recordings</button><button class="sm" aria-pressed={archived} onclick={()=>archived=true}>Archived recordings</button></div>
      <label for="recording-search">Search commands &amp; terminal output</label>
      <input id="recording-search" type="search" placeholder="Search this engagement…" bind:value={query}/>
      <div class="scope"><label><input type="checkbox" bind:checked={regex}/> Regex</label><label><input type="checkbox" bind:checked={matchCase}/> Match case</label></div>
      <p class="status" role="status">{#if loading}Loading command index…{:else if searching}Searching…{:else if query}{hitCount} hits{truncated?' · results limited':''}{:else}{commands.length} commands · {shells.filter(s=>!s.background).length} shells{/if}</p>
      {#if searchError}<p class="error" role="alert">{searchError}</p>{/if}
    </div>
    <div class="timeline-list" bind:this={list} onscroll={readPosition}>
      {#if error}<div class="empty error" role="alert">{error}<button onclick={refresh}>Retry</button></div>
      {:else if !loading && !query && !commands.length}<div class="empty"><strong>No indexed commands yet</strong><p>New recordings appear here with shell identity and timestamps. Existing captures remain in Recordings &amp; edits.</p><button onclick={()=>setParams({mode:'recordings'})}>Browse recordings</button></div>
      {:else if query}
        {#if !searching&&!searchError&&!groups.length}<div class="empty">No matching commands or output.</div>{/if}
        {#each groups as group, gi (`${group.sub}-${gi}`)}
          <div class="search-group">
            {#if group.command}{@const c=group.command}<button class="timeline-command" class:chosen={selected?.id===c.id} onclick={()=>open(c.sub,c.t,c)}><span class="command-meta">{clock(c.t)} · {c.label}{#if c.tag} · {c.tag}{/if}{#if c.exit!==null&&c.exit!==0}<span class="failed">Exit {c.exit}</span>{/if}</span><code><Highlight text={c.cmd} q={regex?'':query}/></code></button>{:else}<p class="command-meta orphan">{group.label} · outside indexed command</p>{/if}
            {#each group.hits as hit, hi}<button class="search-hit" aria-label={`Replay search result ${gi+1}.${hi+1}`} onclick={()=>open(group.sub,hit.t,group.command)}><time>{clock(hit.t)}</time><code><Highlight text={hit.text} q={regex?'':query}/></code>{#if hit.screen}<span class="screen-tag">Screen</span>{/if}</button>{/each}
          </div>
        {/each}
      {:else}
        {#each days as [date, items] (date)}
          <section class="day-group"><button class="day-title" aria-expanded={expandedDays.has(date)} onclick={()=>toggleDay(date)}>{expandedDays.has(date)?'▾':'▸'} {date}<span>{items.length} commands</span></button>
            {#if expandedDays.has(date)}{#each items as c (c.id)}<button class="timeline-command" class:chosen={selected?.id===c.id} aria-label={`Replay ${c.cmd}`} onclick={()=>open(c.sub,c.t,c)}><span class="command-meta"><time>{clock(c.t)}</time><span>{c.label}</span>{#if c.tag}<span>{c.tag}</span>{/if}{#if c.exit===null}<span>Completion unknown</span>{:else if c.exit!==0}<span class="failed">Exit {c.exit}</span>{/if}</span><code>{c.cmd}</code></button>{/each}{/if}
          </section>
        {/each}
      {/if}
    </div>
    <div class="follow-bar"><span>{follow?'Following command index':newCount?`${newCount} new commands`:'Following paused'}</span><button class="sm ghost" onclick={()=>follow?(follow=false,countAtPause=commands.length):resume()}>{follow?'Pause following':'Resume following'}</button></div>
  </aside>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
  <div class="timeline-divider" role="separator" aria-label="Resize command timeline" aria-orientation="vertical" aria-valuemin={260} aria-valuemax={Math.max(260,(host?.clientWidth??1000)-380)} aria-valuenow={paneWidth} tabindex="0" onpointerdown={resize} onpointermove={move} onpointerup={saveWidth} onpointercancel={saveWidth} onkeydown={keyResize}></div>
  <main aria-label="Recorded terminal replay">
    <div class="replay-header"><div><strong>{shell?.label??'Terminal replay'}</strong>{#if selectedSub}<span class="shell-id">{selectedSub}</span>{/if}</div>{#if replayTime!==null}<time>{day(replayTime)} · {clock(replayTime)} Berlin</time>{/if}<ActionMenu label="Replay colors">{#each palettes as palette}<button role="menuitemradio" aria-checked={terminalTheme===palette} onclick={()=>changePalette(palette)}>{terminalTheme===palette?'✓ ':''}{palette.replaceAll('-',' ')}</button>{/each}</ActionMenu></div>
    {#if selected}<div class="selected-command"><code>{selected.cmd}</code><CopyButton text={selected.cmd} title="Copy recorded command"/><div class="command-context"><code>{selected.cwd||'Working directory unavailable'}</code><span>{selected.exit===null?'Completion unknown':`Exit ${selected.exit}`}</span><button class="sm ghost" onclick={loadOutput}>{showOutput?'Hide output':'Command output'}</button><button class="sm ghost" onclick={()=>setParams({mode:'recordings',id:selected!.sessionId??selected!.sub,archived:archived?'1':''})}>Edit recording</button></div></div>{/if}
    {#if replayError}<div role="alert" class="error">{replayError}<button class="sm" onclick={()=>selectedSub?open(selectedSub,selectedTime,selected):selected&&open(selected.sub,selectedTime,selected)}>Retry replay</button></div>{/if}
    {#if preparing}<div role="status" class="status">Preparing recording…</div>{/if}
    <div class="player" bind:this={playerTarget} class:opened={!!info}></div>
    {#if !info&&!preparing&&!replayError}<div class="replay-empty empty"><strong>Select a command or search hit</strong><p>Replay opens at the recorded moment, paused.</p></div>{/if}
    {#if showOutput}<section class="command-output"><div><strong>Complete command output</strong>{#if !outputLoading&&!outputError}<CopyButton text={output} title="Copy complete command output"/>{/if}</div>{#if outputLoading}<p role="status">Loading output…</p>{:else if outputError}<p role="alert" class="error">{outputError}</p>{:else}<pre>{output||'No output'}</pre>{/if}</section>{/if}
    <p class="replay-help">Recorded playback · <kbd>Space</kbd> play/pause · <kbd>←</kbd> <kbd>→</kbd> 5 seconds · <kbd>n</kbd> <kbd>p</kbd> next/previous command · full-screen search uses the final screen snapshot</p>
  </main>
</div>
{#if allowLarge}<Modal title="Open large recording?" onclose={()=>allowLarge=null}><p>This recording is {(allowLarge.bytes/1048576).toFixed(0)} MiB. Preparing it may take a while.</p><button onclick={()=>{const sub=allowLarge!.sub;allowLarge=null;open(sub,selectedTime,selected,true);}}>Prepare recording</button></Modal>{/if}
<style>
 .timeline { height:100%; min-height:0; display:grid; grid-template-columns:minmax(260px,var(--timeline-width)) 7px minmax(0,1fr); }
 aside { min-height:0; display:flex; flex-direction:column; background:var(--pd-sidebar); }
 .timeline-tools { padding:12px; border-bottom:1px solid var(--pd-divider); }
 .scope { display:flex; align-items:center; gap:12px; margin-bottom:8px; flex-wrap:wrap; }
 .scope button[aria-pressed=true] { background:var(--pd-selected);border-color:var(--pd-accent);color:var(--pd-text); }
 .scope label { display:flex; align-items:center; gap:6px; }
 label,.status,.command-meta,.follow-bar,.replay-help,.shell-id { font-size:.8125rem; color:var(--pd-text-muted); }
 #recording-search { width:100%; margin:6px 0 8px; }
 .status { margin:4px 0 0; }.error { color:var(--pd-danger); padding:12px; }
 .timeline-list { flex:1; min-height:0; overflow:auto; overflow-anchor:none; }
 .day-title { position:sticky; top:0; z-index:1; border:0; border-radius:0; background:var(--pd-sidebar); width:100%; display:flex; gap:8px; justify-content:flex-start; border-bottom:1px solid var(--pd-divider); padding:8px 12px; font-weight:600; }
 .day-title span { margin-left:auto; font-size:.8125rem; font-weight:400; color:var(--pd-text-muted); }
 .timeline-command { display:block; text-align:left; width:100%; min-height:52px; padding:8px 12px; border:0; border-radius:0; border-left:3px solid transparent; background:transparent; }
 .timeline-command:hover,.search-hit:hover { background:var(--pd-hover); }.timeline-command.chosen { background:var(--pd-selected); border-left-color:var(--pd-accent); }
 .command-meta { display:flex; flex-wrap:wrap; gap:4px 8px; margin-bottom:4px; font-variant-numeric:tabular-nums; }.failed {color:var(--pd-danger);}
 .timeline-command code,.search-hit code { white-space:pre-wrap; overflow-wrap:anywhere; font-size:.875rem; line-height:1.5; background:transparent; padding:0; }
 .search-group { padding-bottom:8px; }.search-hit { width:100%; display:flex; text-align:left; align-items:baseline; gap:8px; padding:6px 12px 6px 24px; border:0; border-radius:0; background:transparent; }.search-hit time,.screen-tag { font-size:.75rem; color:var(--pd-text-muted); }.search-hit code { min-width:0; }.orphan {padding:8px 12px;}
 .follow-bar { display:flex; align-items:center; flex-wrap:wrap; justify-content:space-between; gap:4px; padding:6px 12px; border-top:1px solid var(--pd-divider); }
 .timeline-divider { cursor:col-resize; border-left:1px solid var(--pd-divider); touch-action:none; }.timeline-divider:hover {background:var(--pd-hover);}.dragging {user-select:none;}
 main { min-width:0; min-height:0; overflow:auto; position:relative; display:flex; flex-direction:column; padding:12px 16px; gap:12px; background:var(--pd-bg); }
 .replay-header {display:flex; align-items:center; flex-wrap:wrap; gap:8px;}.replay-header>div {min-width:0;}.shell-id {display:block;overflow-wrap:anywhere;margin-top:4px;}.replay-header time {color:var(--pd-accent);font-size:.8125rem;margin-left:auto;font-variant-numeric:tabular-nums;}
 .selected-command { display:flex; align-items:flex-start; flex-wrap:wrap; gap:8px; padding:12px; border:1px solid var(--pd-divider); border-radius:6px; background:var(--pd-surface); }.selected-command>code {flex:1;min-width:0;white-space:pre-wrap;overflow-wrap:anywhere;}.command-context {width:100%;display:flex;align-items:center;gap:8px;flex-wrap:wrap;font-size:.8125rem;color:var(--pd-text-muted);}.command-context code {overflow-wrap:anywhere;}
 .player { flex:1; min-height:260px; min-width:0; display:flex; overflow:hidden; background:var(--pd-terminal); border-radius:6px; }.player:not(.opened){display:none;}.player :global(>*) {flex:1;min-width:0;}.player :global(.ap-overlay-start){display:none;}
 .replay-empty {margin:auto;}.replay-help {margin:0;line-height:1.7;}kbd {font:inherit;border:1px solid var(--pd-divider);border-radius:3px;padding:1px 4px;}
 .command-output {background:var(--pd-terminal);padding:12px;border:1px solid var(--pd-divider);border-radius:6px;}.command-output>div{display:flex;align-items:center;justify-content:space-between;}.command-output pre {max-height:300px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;font-size:.875rem;line-height:1.5;}
 :global([data-density=compact]) .timeline-command {padding-block:6px;min-height:44px;}
 @media(max-width:900px){.timeline{grid-template-columns:1fr;grid-template-rows:minmax(220px,45%) minmax(0,1fr);}.timeline-divider{display:none;}main{padding:12px;}aside{border-bottom:1px solid var(--pd-divider);}.replay-help{font-size:.75rem;}}
</style>
