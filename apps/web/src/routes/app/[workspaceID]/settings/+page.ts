export const prerender = false;
export const ssr = false;

export function load({ params }: { params: { workspaceID: string } }) {
  return { workspaceID: params.workspaceID };
}
