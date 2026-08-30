import type { IndexStatus, Me, ReviewItem, Source, SourceFile, Task } from './types';

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  // 401 处理：/api/auth/me 是挂载时的登录态探测（未登录属预期，交给 App 渲染
  // 登录页，不得触发整页重定向，否则与 useEffect 形成重载风暴）；
  // 其余端点的 401 视为会话过期 → 跳登录页（已在 /login 时不再跳转）。
  if (res.status === 401 && !path.startsWith('/api/auth/login') && !path.startsWith('/api/auth/me')) {
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    throw new Error('未登录');
  }
  const text = await res.text();
  if (!res.ok) {
    let msg = text;
    try {
      msg = JSON.parse(text).error ?? text;
    } catch {
      /* keep raw */
    }
    throw new Error(msg);
  }
  return (text ? JSON.parse(text) : null) as T;
}

export const api = {
  me: () => req<Me>('GET', '/api/auth/me'),
  login: (username: string, password: string) => req<Me>('POST', '/api/auth/login', { username, password }),
  logout: () => req('POST', '/api/auth/logout'),

  sources: () => req<Source[]>('GET', '/api/sources'),
  createSource: (name: string, path: string, kind: string) =>
    req<Source>('POST', '/api/sources', { name, path, kind }),
  updateSource: (id: number, patch: Partial<Source>) => req('PUT', `/api/sources/${id}`, patch),
  deleteSource: (id: number) => req('DELETE', `/api/sources/${id}`),
  scanSource: (id: number) => req<{ task_id: number }>('POST', `/api/sources/${id}/scan`),
  files: (id: number) => req<SourceFile[]>(`GET`, `/api/sources/${id}/files`),

  review: (state: string) => req<ReviewItem[]>('GET', `/api/review?state=${encodeURIComponent(state)}`),

  tasks: () => req<Task[]>('GET', '/api/tasks?limit=50'),
  indexStatus: () => req<IndexStatus>('GET', '/api/index'),
  refreshIndex: (localPath: string) =>
    req<{ task_id: number }>('POST', '/api/index/refresh', { local_path: localPath }),
};
