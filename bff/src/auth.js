import { createRemoteJWKSet, jwtVerify } from 'jose';

export function createAuth({ issuer = process.env.KEYCLOAK_ISSUER, jwks } = {}) {
  let remoteJwks;
  
  const getJwks = () => {
    remoteJwks ??= createRemoteJWKSet(new URL(process.env.KEYCLOAK_JWKS_URL));
    return remoteJwks;
  };

  return async function requireAuth(req, res, next) {
    const [scheme, token] = (req.get('authorization') ?? '').split(' ');
    if (scheme?.toLowerCase() !== 'bearer' || !token) return unauthorized(res);

    try {
      const { payload } = await jwtVerify(token, jwks ?? getJwks(), {
        issuer,
        algorithms: ['RS256'],
      });
      if (!payload.sub) return unauthorized(res);
      req.user = { id: payload.sub };
      next();
    } catch {
      unauthorized(res);
    }
  };
}

function unauthorized(res) {
  return res.status(401).json({
    error: { code: 'UNAUTHORIZED', message: 'Token ausente, inválido ou expirado' },
  });
}
