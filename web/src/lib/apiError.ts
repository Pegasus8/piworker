// What a failed request says: the API envelope's error ({ success: false,
// error }) and the HTTP status. Pure helpers shared by the stores, the
// dialogs and the lib modules that read a server's refusal.

/** The server's error message, or fallback. */
export function apiErrorMessage(e: unknown, fallback: string): string {
  const message = (e as { response?: { data?: { error?: unknown } } })?.response?.data?.error
  return typeof message === 'string' && message ? message : fallback
}

/** The HTTP status of a failed request (undefined when the server did not answer). */
export function apiStatus(e: unknown): number | undefined {
  return (e as { response?: { status?: number } })?.response?.status
}
