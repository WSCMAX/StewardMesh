async page => {
  const assert = (condition, message) => {
    if (!condition) throw new Error(message)
  }
  const browserProblems = []
  page.on('console', message => {
    if (message.type() === 'warning' || message.type() === 'error') browserProblems.push(`${message.type()}: ${message.text()}`)
  })
  page.on('pageerror', error => browserProblems.push(`pageerror: ${error.message}`))

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('http://127.0.0.1:15173/#workspace-atlas')
  await page.getByRole('heading', { name: 'Create the first administrator' }).waitFor()
  await page.getByLabel('Display name').fill('Phase One Administrator')
  await page.getByLabel('Email address').fill('phase-one-admin@example.test')
  await page.getByLabel('Username').fill('phase-one-admin')
  await page.getByLabel('Password', { exact: true }).fill('Phase-one-admin-password!')
  await page.getByLabel('Confirm password').fill('Phase-one-admin-password!')
  await page.getByRole('button', { name: 'Create administrator' }).click()
  await page.locator('#assets-heading').waitFor()

  const bootstrapState = await page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/bootstrap', { credentials: 'same-origin' })
    return { status: response.status, body: await response.json() }
  })
  assert(bootstrapState.status === 200, `unexpected bootstrap status ${bootstrapState.status}`)
  assert(bootstrapState.body.required === false, 'administrator bootstrap remained available')
  assert(browserProblems.length === 0, `browser diagnostics: ${browserProblems.join(' | ')}`)
  return { scenario: 'bootstrap-admin', status: 'passed' }
}
