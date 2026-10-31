import type { IconLimits } from '@/config'

// techPattern is a technology as the server takes it, lowercased.
export const techPattern = /^[a-z0-9][a-z0-9._+-]{0,62}$/

// limitsText tells the limits of a logo: « PNG, JPEG, WebP ou SVG · 256 Ko
// au plus · 16 à 512 px de côté ».
export function limitsText(l: IconLimits): string {
  return `PNG, JPEG, WebP ou SVG · ${Math.round(l.maxSize / 1024)} Ko au plus · ${l.minSide} à ${l.maxSide} px de côté`
}

// precheck refuses before the upload a file the server would refuse for
// its extension or its weight; its sides are checked by the server.
export function precheck(file: { name: string; size: number }, l: IconLimits): string | null {
  const ext = file.name.includes('.') ? file.name.slice(file.name.lastIndexOf('.')).toLowerCase() : ''
  if (!l.extensions.includes(ext)) return 'Le fichier doit être une image PNG, JPEG, WebP ou SVG.'
  if (file.size > l.maxSize) return `Le fichier pèse ${Math.ceil(file.size / 1024)} Ko : ${Math.round(l.maxSize / 1024)} Ko au plus.`
  return null
}

// refusal says in French why the server refused a logo; fromLacis tells
// an answer of lacis-server, which carries an error, from one of a proxy
// or an older server without the icon routes.
export function refusal(status: number, reason: string | undefined, l: IconLimits, fromLacis = true): string {
  switch (reason) {
    case 'too_big': return `Le fichier est trop lourd : ${Math.round(l.maxSize / 1024)} Ko au plus.`
    case 'format': return 'Le fichier doit être une image PNG, JPEG, WebP ou SVG, avec l\'extension de son format.'
    case 'dimension': return `Chaque côté de l'image doit faire entre ${l.minSide} et ${l.maxSide} px.`
    case 'unsafe': return 'Ce SVG contient du script ou charge du contenu externe : il est refusé.'
    case 'tech': return 'Nom de techno invalide : lettres minuscules, chiffres et . _ + -.'
  }
  if (status === 404 && !fromLacis) return 'Le serveur ne connaît pas l\'envoi de logos : il n\'est pas à jour, ou un proxy ne lui transmet pas icons/.'
  if (status === 403) return 'Vous n\'avez pas le droit de changer les logos.'
  if (status === 401) return 'Votre session a expiré : reconnectez-vous.'
  return 'Le logo n\'a pas pu être enregistré.'
}

// sendIcon uploads the file as the logo of the technology, or removes it
// without file; it returns why it was refused, null once done.
export async function sendIcon(tech: string, l: IconLimits, file?: File): Promise<string | null> {
  if (file) {
    const refused = precheck(file, l)
    if (refused) return refused
  }
  let body: FormData | undefined
  if (file) {
    body = new FormData()
    body.append('file', file)
  }
  try {
    const res = await fetch(`icons/${encodeURIComponent(tech)}`, { method: file ? 'PUT' : 'DELETE', body, credentials: 'same-origin' })
    if (res.ok) return null
    const data = (await res.json().catch(() => ({}))) as { error?: string; reason?: string }
    return refusal(res.status, data.reason, l, data.error !== undefined)
  } catch {
    return 'Le serveur ne répond pas.'
  }
}
