import process from 'node:process'

const GH_TOKEN_URL = 'https://github.com/login/oauth/access_token'
const GH_API = 'https://api.github.com'

export interface GitHubUser {
  id: string // numeric, as string
  login: string
  name: string | null
  avatar_url: string
}

export async function exchangeCodeForToken(code: string): Promise<string> {
  const res = await fetch(GH_TOKEN_URL, {
    method: 'POST',
    headers: { 'Accept': 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_id: process.env.GITHUB_CLIENT_ID,
      client_secret: process.env.GITHUB_CLIENT_SECRET,
      code,
    }),
  })
  if (!res.ok)
    throw new Error(`GitHub token exchange returned ${res.status}`)
  const data = await res.json() as { access_token?: string, error?: string }
  if (!data.access_token)
    throw new Error(data.error ?? 'No access_token returned')
  return data.access_token
}

export async function getGitHubUser(accessToken: string): Promise<GitHubUser> {
  const res = await fetch(`${GH_API}/user`, {
    headers: { 'Authorization': `Bearer ${accessToken}`, 'User-Agent': 'claude-agent-dashboard' },
  })
  if (!res.ok)
    throw new Error(`GitHub /user returned ${res.status}`)
  const u = await res.json() as { id: number, login: string, name: string | null, avatar_url: string }
  return { id: String(u.id), login: u.login, name: u.name, avatar_url: u.avatar_url }
}

/**
 * Returns true when GITHUB_ORG is unset (no restriction) or the user is a member.
 * Uses GITHUB_SERVER_TOKEN for private-membership orgs unless GITHUB_ORG_MEMBERSHIP_PUBLIC=true.
 */
export async function isOrgMember(githubLogin: string, userAccessToken: string): Promise<boolean> {
  const org = process.env.GITHUB_ORG
  if (!org)
    return true

  let token: string
  if (process.env.GITHUB_ORG_MEMBERSHIP_PUBLIC === 'true') {
    token = userAccessToken
  }
  else if (process.env.GITHUB_SERVER_TOKEN) {
    token = process.env.GITHUB_SERVER_TOKEN
  }
  else {
    console.warn('[auth] GITHUB_ORG is set but GITHUB_SERVER_TOKEN is missing — falling back to user token. Private org membership checks will likely fail.')
    token = userAccessToken
  }

  const res = await fetch(`${GH_API}/orgs/${org}/members/${githubLogin}`, {
    headers: { 'Authorization': `Bearer ${token}`, 'User-Agent': 'claude-agent-dashboard' },
  })
  return res.status === 204
}
