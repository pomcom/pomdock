// Capture the invoking control even on browsers that do not focus clicked
// buttons. Keep this transient; keyboard-opened overlays use activeElement.
let invoker: HTMLElement | null = null;
export function trackActivation(event: MouseEvent) {
  const target = event.target instanceof Element ? event.target.closest<HTMLElement>('button, a[href], input, select, summary') : null;
  invoker = target;
  setTimeout(() => { if (invoker === target) invoker = null; }, 0);
}
export function returnFocusTarget(): HTMLElement | null {
  return invoker?.isConnected ? invoker : document.activeElement as HTMLElement | null;
}
