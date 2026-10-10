// Base URL for API calls. In dev we go through the Vite proxy (/api → backend);
// in a production build we call the deployed API Gateway URL directly.
export const API_BASE = import.meta.env.DEV ? '/api' : import.meta.env.VITE_API_URL;

export type Health = { status: string; stage: string };

export async function getHealth(): Promise<Health> {
  const res = await fetch(`${API_BASE}/health`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
