async page => {
  const assert = (condition, message) => {
    if (!condition) throw new Error(message)
  }
  const browserProblems = []
  const expectedConsoleErrors = { asset403: 0, identifier404: 0 }
  const consumedConsoleErrors = { asset403: 0, identifier404: 0 }
  page.on('console', message => {
    const text = message.text()
    const location = message.location().url
    if (message.type() === 'error' && expectedConsoleErrors.asset403 > 0 &&
      text === 'Failed to load resource: the server responded with a status of 403 (Forbidden)' &&
      location === 'http://127.0.0.1:15173/api/v1/assets') {
      expectedConsoleErrors.asset403 -= 1
      consumedConsoleErrors.asset403 += 1
      return
    }
    if (message.type() === 'error' && expectedConsoleErrors.identifier404 > 0 &&
      text === 'Failed to load resource: the server responded with a status of 404 (Not Found)' &&
      location === 'http://127.0.0.1:15173/api/v1/asset-identifiers/resolve') {
      expectedConsoleErrors.identifier404 -= 1
      consumedConsoleErrors.identifier404 += 1
      return
    }
    if (message.type() === 'warning' || message.type() === 'error') browserProblems.push(`${message.type()}: ${text}`)
  })
  page.on('pageerror', error => browserProblems.push(`pageerror: ${error.message}`))

  await page.setViewportSize({ width: 320, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('http://127.0.0.1:15173/#workspace-people')
  await page.getByRole('heading', { name: 'Sign in to StewardMesh' }).waitFor()
  await page.getByLabel('Username').fill('phase-one-reader')
  await page.getByLabel('Password', { exact: true }).fill('Phase-one-reader-password!')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.locator('#people-heading').waitFor()

  const session = await page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/session', { credentials: 'same-origin' })
    return response.json()
  })
  assert(JSON.stringify(session.permissions) === JSON.stringify(['assets.read', 'directory.read', 'organization.read']), `unexpected reader permissions ${JSON.stringify(session.permissions)}`)
  assert(session.principal.roles.includes('Phase One Reader'), 'reader role is missing from the real session')

  await page.getByRole('tab', { name: 'Workflows & assignments' }).click()
  await page.getByRole('note').filter({ hasText: 'cannot add a person' }).waitFor()
  assert(await page.getByRole('button', { name: 'Start person workflow' }).count() === 0, 'reader can start the People write workflow')
  assert(await page.getByText('Add a site', { exact: true }).count() === 0, 'reader can create a site')
  assert(await page.getByText(/Directory creation controls remain unavailable until an administrator grants/).count() === 1, 'reader fallback does not explain the missing write grant')

  const menu = page.getByRole('button', { name: 'Open workspace navigation' })
  await menu.click()
  const navigation = page.getByRole('dialog', { name: 'Workspace navigation' })
  await navigation.waitFor()
  const close = navigation.getByRole('button', { name: 'Close workspace navigation' })
  assert(await close.evaluate(element => element === document.activeElement), 'mobile navigation did not focus Close')
  await page.keyboard.press('Shift+Tab')
  assert(await navigation.getByRole('button', { name: 'Report an issue' }).evaluate(element => element === document.activeElement), 'mobile navigation did not wrap backward')
  await page.keyboard.press('Tab')
  assert(await close.evaluate(element => element === document.activeElement), 'mobile navigation did not wrap forward')
  await page.keyboard.press('Escape')
  assert(await navigation.count() === 0, 'Escape did not close mobile navigation')
  assert(await menu.evaluate(element => element === document.activeElement), 'mobile navigation did not restore focus')

  await menu.click()
  await page.getByRole('dialog', { name: 'Workspace navigation' }).getByRole('link', { name: /^Atlas —/ }).click()
  await page.locator('#assets-heading').waitFor()
  assert(await page.getByRole('button', { name: 'Add asset' }).count() === 0, 'reader can add an asset')
  assert(await page.getByRole('button', { name: 'Print labels' }).count() === 0, 'reader can open label printing')
  await page.getByRole('tab', { name: 'Scan' }).click()
  const scannerPanel = page.locator('section[aria-labelledby="atlas-scanner-heading"]')
  await scannerPanel.getByRole('button', { name: 'Open scanner', exact: true }).click()
  const scannerForm = scannerPanel.getByRole('form', { name: 'Scan an Atlas Code', exact: true })
  const scannerWorkflow = scannerForm.locator('label').filter({ hasText: /^Workflow/ }).locator('select')
  assert(await scannerWorkflow.locator('option[value="associate"]').count() === 0, 'reader scanner exposes association mode')

  expectedConsoleErrors.asset403 = 1
  expectedConsoleErrors.identifier404 = 1
  const deniedResponses = await page.evaluate(async csrfToken => {
    const write = await fetch('/api/v1/assets', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify({ name: 'Must Not Exist', kind: 'server', status: 'draft' }),
    })
    const writeBody = await write.json()
    const missing = await fetch('/api/v1/asset-identifiers/resolve', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ symbology: 'code128', value: 'E2E-UNKNOWN-CODE' }),
    })
    return { writeStatus: write.status, writeBody, missingStatus: missing.status, missingBody: await missing.json() }
  }, session.csrfToken)
  assert(deniedResponses.writeStatus === 403, `reader asset write returned ${deniedResponses.writeStatus}`)
  assert(deniedResponses.writeBody.error.code === 'permission_denied', 'reader write did not return permission_denied')
  assert(!JSON.stringify(deniedResponses.writeBody).includes('Must Not Exist'), 'denied response reflected private input')
  assert(deniedResponses.missingStatus === 404, `unknown code returned ${deniedResponses.missingStatus}`)
  assert(deniedResponses.missingBody.error.message === 'the requested asset identifier was not found', 'unknown code response was not generic')

  await page.addScriptTag({ path: 'web/node_modules/axe-core/axe.min.js' })
  const violations = await page.evaluate(async () => (await globalThis.axe.run(document)).violations.map(violation => `${violation.id}:${violation.nodes.length}`))
  assert(violations.length === 0, `reader mobile axe violations: ${violations.join(', ')}`)
  const width = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }))
  assert(width.scroll <= width.client, `reader mobile overflowed: ${width.scroll} > ${width.client}`)
  assert(consumedConsoleErrors.asset403 === 1 && expectedConsoleErrors.asset403 === 0, 'controlled reader 403 console budget was not consumed exactly once')
  assert(consumedConsoleErrors.identifier404 === 1 && expectedConsoleErrors.identifier404 === 0, 'controlled identifier 404 console budget was not consumed exactly once')
  assert(browserProblems.length === 0, `browser diagnostics: ${browserProblems.join(' | ')}`)
  return { scenario: 'reader-denied-mobile', status: 'passed', axe: '0 violations', overflow: false }
}
