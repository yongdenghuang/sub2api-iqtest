/** Compact user-facing reason; retain the original message in the details tooltip. */
export function accountErrorReason(message?: string | null): string {
  const text = (message || '').toLowerCase()
  if (/token_revoked|token_expired|refresh_token_reused|invalid_grant|token.{0,20}expired|token.{0,20}revoked/.test(text)) return 'auth_expired'
  if (/account.{0,20}(deactivated|suspended|disabled|banned)/.test(text)) return 'account_blocked'
  if (/model_not_found|model.{0,100}(not supported|not found|not available|does not exist)/.test(text)) return 'model_unsupported'
  if (/insufficient_quota|quota exceeded|usage limit|credit balance|out of credits/.test(text)) return 'quota_exhausted'
  if (/\b401\b|unauthorized|authentication|no access token|invalid.*token|access_token not found/.test(text)) return 'auth_error'
  if (/\b403\b|forbidden|permission denied/.test(text)) return 'permission_denied'
  if (/\b429\b|rate.?limit|too many requests/.test(text)) return 'rate_limited'
  if (/timeout|timed out|deadline exceeded/.test(text)) return 'timeout'
  if (/\b5\d\d\b/.test(text)) return 'upstream_5xx'
  if (/connection|connect:|network|dial tcp|request failed/.test(text)) return 'network_error'
  return 'unknown_error'
}
