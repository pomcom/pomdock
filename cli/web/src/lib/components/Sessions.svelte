<script lang="ts">
  import { tick } from 'svelte';
  import { route, setParams } from '../router.svelte';
  import RecordingLibrary from './RecordingLibrary.svelte';
  import RecordingTimeline from './RecordingTimeline.svelte';
  const id=$props.id();
  let tabRoot:HTMLDivElement;
  async function tabKeys(event:KeyboardEvent){
    if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
    event.preventDefault();
    const next=event.key==='Home'?false:event.key==='End'?true:!library;
    setParams({mode:next?'recordings':'timeline',...(next?{}:{id:''})});
    await tick();tabRoot.querySelector<HTMLButtonElement>('[aria-selected=true]')?.focus();
  }
  const library = $derived(route.params.mode === 'recordings' || !!route.params.id);
</script>
<div class="session-workspace">
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="session-tabs" role="tablist" aria-label="Session view" tabindex="-1" bind:this={tabRoot} onkeydown={tabKeys}>
    <button id={`${id}-timeline`} role="tab" aria-controls={`${id}-panel`} tabindex={library?-1:0} aria-selected={!library} onclick={() => setParams({mode: 'timeline', id: ''})}>Timeline &amp; replay</button>
    <button id={`${id}-recordings`} role="tab" aria-controls={`${id}-panel`} tabindex={library?0:-1} aria-selected={library} onclick={() => setParams({mode: 'recordings'})}>Recordings &amp; edits</button>
  </div>
  <div class="session-content" id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-${library?'recordings':'timeline'}`}>
    {#if library}<RecordingLibrary />{:else}<RecordingTimeline />{/if}
  </div>
</div>
<style>
 .session-workspace { height:100%; min-height:0; display:flex; flex-direction:column; }
 .session-tabs { display:flex; flex-wrap:wrap; gap:4px; padding:4px 16px; border-bottom:1px solid var(--pd-divider); background:var(--pd-sidebar); }
 .session-tabs button { border-color:transparent; background:transparent; color:var(--pd-text-muted); }
 .session-tabs button[aria-selected=true] { color:var(--pd-text); background:var(--pd-hover); }
 .session-content { flex:1; min-height:0; }
</style>
