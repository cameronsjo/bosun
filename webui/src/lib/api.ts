// API Types
export interface APIStatus {
  health: string;
  state: string;
  uptime: string;
  uptime_seconds: number;
  last_reconcile?: string;
  last_error?: string;
  next_poll?: string;
  poll_interval?: number;
}

export interface Container {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  health?: string;
  created: string;
  ports?: string[];
}

export interface ContainerSummary {
  total: number;
  running: number;
  stopped: number;
  unhealthy: number;
}

export interface ContainersResponse {
  containers: Container[];
  summary: ContainerSummary;
}

export interface LogsResponse {
  container: string;
  lines: number;
  logs: string;
}

export interface TriggerResponse {
  status: string;
  message: string;
}

export interface RestartResponse {
  status: string;
  container: string;
  message: string;
}

// API Error type
export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
    public isAuthError: boolean = false
  ) {
    super(message);
    this.name = 'APIError';
  }
}

// Get bearer token from window config (injected at runtime)
function getBearerToken(): string {
  // In production, this is injected by the nginx entrypoint
  // In development, use env var or empty string
  const windowConfig = (window as unknown as { BOSUN_CONFIG?: { bearerToken?: string } }).BOSUN_CONFIG;
  return windowConfig?.bearerToken || '';
}

// API fetch wrapper with auth
async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = getBearerToken();

  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...options.headers,
  };

  if (token) {
    (headers as Record<string, string>)['Authorization'] = `Bearer ${token}`;
  }

  const response = await fetch(path, {
    ...options,
    headers,
  });

  if (!response.ok) {
    const isAuthError = response.status === 401 || response.status === 403;
    const message = await response.text().catch(() => response.statusText);
    throw new APIError(message, response.status, isAuthError);
  }

  return response.json();
}

// API client
export const api = {
  // Get daemon status
  async getStatus(): Promise<APIStatus> {
    return apiFetch<APIStatus>('/api/status');
  },

  // Get all containers
  async getContainers(): Promise<ContainersResponse> {
    return apiFetch<ContainersResponse>('/api/containers');
  },

  // Get container logs
  async getContainerLogs(containerId: string, lines: number = 100): Promise<LogsResponse> {
    return apiFetch<LogsResponse>(`/api/containers/${containerId}/logs?lines=${lines}`);
  },

  // Restart a container
  async restartContainer(containerId: string): Promise<RestartResponse> {
    return apiFetch<RestartResponse>(`/api/containers/${containerId}/restart`, {
      method: 'POST',
    });
  },

  // Trigger reconciliation
  async trigger(source: string = 'webui'): Promise<TriggerResponse> {
    return apiFetch<TriggerResponse>('/api/trigger', {
      method: 'POST',
      body: JSON.stringify({ source }),
    });
  },
};
