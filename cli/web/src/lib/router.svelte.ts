// Hash router: #/<view>?name=<engagement>&id=<session>. Hash-based so every
// view is a deep link without server-side routes.

export type View = 'tracker' | 'sessions' | 'containers' | 'shells' | 'help';
const VIEWS: View[] = ['tracker', 'sessions', 'containers', 'shells', 'help'];

type Route = { view: View; params: Record<string, string> };

function parse(): Route {
  const raw = location.hash.replace(/^#\/?/, '');
  const [path, qs = ''] = raw.split('?');
  const view = (VIEWS as string[]).includes(path) ? (path as View) : 'tracker';
  return { view, params: Object.fromEntries(new URLSearchParams(qs)) };
}

export const route: Route = $state(parse());

window.addEventListener('hashchange', () => {
  const next = parse();
  route.view = next.view;
  route.params = next.params;
});

function hashFor(view: View, params: Record<string, string>): string {
  const qs = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '')).toString();
  return `#/${view}${qs ? `?${qs}` : ''}`;
}

export function navigate(view: View, params: Record<string, string> = {}) {
  location.hash = hashFor(view, params);
}

/** Update query params in place, without a new history entry. */
export function setParams(params: Record<string, string>) {
  route.params = { ...route.params, ...params };
  history.replaceState(null, '', hashFor(route.view, route.params));
}
