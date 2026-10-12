<script lang="ts">
  import { api } from '../api';
  import type { Job } from '../types';

  // Shows one background job's output, polling while it runs.
  let { id, ondone }: { id: string; ondone?: (job: Job) => void } = $props();

  let job: Job | null = $state(null);
  let error = $state('');
  let pre: HTMLPreElement | undefined = $state();

  $effect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      try {
        const j = await api.job(id);
        if (stopped) return;
        const follow = pre ? pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8 : true;
        job = j;
        if (follow) queueMicrotask(() => pre && (pre.scrollTop = pre.scrollHeight));
        if (j.status === 'running') timer = setTimeout(tick, 800);
        else ondone?.(j);
      } catch (e) {
        error = (e as Error).message;
      }
    };
    tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  });
</script>

{#if error}
  <div class="err">{error}</div>
{:else if job}
  <pre class="log" bind:this={pre}>{job.output || (job.status === 'running' ? 'waiting for output…' : '(no output)')}</pre>
  {#if job.error}<div class="err">{job.error}</div>{/if}
{/if}

<style>
  .err { color: var(--red); font-size: 12px; margin-top: 6px; font-family: var(--mono); white-space: pre-wrap; }
</style>
