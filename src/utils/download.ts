export function triggerDownload(url: string, filename?: string): void {
  const anchor = document.createElement('a')
  anchor.href = url
  if (filename)
    anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
}
