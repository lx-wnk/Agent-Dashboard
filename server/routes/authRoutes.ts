import process from 'node:process'
import { Router } from 'express'
import { exchangeCodeForToken, getGitHubUser, isOrgMember } from '../auth/githubOAuth.js'
import { signJwt } from '../auth/jwtUtils.js'
import { isAuthEnabled, requireAuth } from '../auth/requireAuth.js'
import { upsertUser } from '../db/usersRepo.js'

interface AuthRouterDeps {
  host: string
  port: number
}

export function createAuthRouter({ host, port }: AuthRouterDeps): Router {
  const router = Router()

  router.get('/auth/login', (_req, res) => {
    if (!isAuthEnabled()) {
      res.redirect('/')
      return
    }
    // DASHBOARD_PUBLIC_URL allows reverse-proxy deployments to use https://
    const publicBase = process.env.DASHBOARD_PUBLIC_URL?.replace(/\/$/, '')
    const redirectUri = publicBase
      ? `${publicBase}/auth/callback`
      : `http://${host}:${port}/auth/callback`
    const params = new URLSearchParams({
      client_id: process.env.GITHUB_CLIENT_ID!,
      scope: 'read:org',
      redirect_uri: redirectUri,
    })
    res.redirect(`https://github.com/login/oauth/authorize?${params}`)
  })

  router.get('/auth/callback', async (req, res) => {
    const code = req.query.code as string | undefined
    if (!code) {
      res.status(400).send('Missing code')
      return
    }
    try {
      const accessToken = await exchangeCodeForToken(code)
      const ghUser = await getGitHubUser(accessToken)
      const member = await isOrgMember(ghUser.login, accessToken)
      if (!member) {
        res.status(403).send('You must be a member of the required GitHub org to access this dashboard.')
        return
      }
      const jwtSecret = process.env.JWT_SECRET
      if (!jwtSecret) {
        console.error('[auth] JWT_SECRET is not set — refusing to issue session token')
        res.status(500).send('Server misconfiguration: JWT_SECRET is not set')
        return
      }
      const user = upsertUser({ id: ghUser.id, githubLogin: ghUser.login, displayName: ghUser.name, avatarUrl: ghUser.avatar_url })
      const token = signJwt(
        { sub: user.id, login: user.githubLogin, isAdmin: user.isAdmin },
        jwtSecret,
        8 * 3600,
      )
      // Set secure flag when using a public HTTPS URL or a non-loopback host,
      // so the JWT session token is never transmitted over plain HTTP in production.
      const isSecureContext = !!(
        process.env.DASHBOARD_PUBLIC_URL?.startsWith('https://')
        || (host !== '127.0.0.1' && host !== 'localhost')
      )
      res.cookie('dashboard_session', token, {
        httpOnly: true,
        sameSite: 'lax',
        maxAge: 8 * 3600 * 1000,
        secure: isSecureContext,
      })
      res.redirect('/')
    }
    catch (err) {
      console.error('[auth] OAuth callback error:', err)
      res.status(500).send('Authentication failed')
    }
  })

  router.post('/auth/logout', (_req, res) => {
    res.clearCookie('dashboard_session')
    res.redirect('/auth/login')
  })

  router.get('/api/me', requireAuth, (req, res) => {
    if (!isAuthEnabled()) {
      res.json({ user: null, isAdmin: true, authEnabled: false })
      return
    }
    res.json({ user: req.user, isAdmin: req.user?.isAdmin ?? false, authEnabled: true })
  })

  return router
}
