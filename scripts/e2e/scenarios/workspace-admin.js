async page => {
  const assert = (condition, message) => {
    if (!condition) throw new Error(message)
  }
  const browserProblems = []
  const expectedConsoleErrors = { people503: 0 }
  let consumedPeople503 = 0
  page.on('console', message => {
    const text = message.text()
    const location = message.location().url
    if (message.type() === 'error' && expectedConsoleErrors.people503 > 0 &&
      text === 'Failed to load resource: the server responded with a status of 503 (Service Unavailable)' &&
      location === 'http://127.0.0.1:15173/api/v1/identities') {
      expectedConsoleErrors.people503 -= 1
      consumedPeople503 += 1
      return
    }
    if (message.type() === 'warning' || message.type() === 'error') browserProblems.push(`${message.type()}: ${text}`)
  })
  page.on('pageerror', error => browserProblems.push(`pageerror: ${error.message}`))

  const login = async hash => {
    await page.goto(`http://127.0.0.1:15173/${hash}`)
    await page.getByRole('heading', { name: 'Sign in to StewardMesh' }).waitFor()
    await page.getByLabel('Username').fill('phase-one-admin')
    await page.getByLabel('Password', { exact: true }).fill('Phase-one-admin-password!')
    await page.getByRole('button', { name: 'Sign in' }).click()
  }
  const axe = async label => {
    if (!await page.evaluate(() => Boolean(globalThis.axe))) {
      await page.addScriptTag({ path: 'web/node_modules/axe-core/axe.min.js' })
    }
    const violations = await page.evaluate(async () => {
      const result = await globalThis.axe.run(document)
      return result.violations.map(violation => {
        const nodes = violation.nodes.map(node => `${node.target.join(' ')}: ${(node.any[0] && node.any[0].message) || node.failureSummary || ''}`)
        return `${violation.id}[${nodes.join(' | ')}]`
      })
    })
    assert(violations.length === 0, `${label} axe violations: ${violations.join(', ')}`)
  }
  const assertFocused = async (locator, message) => {
    assert(await locator.evaluate(element => element === document.activeElement), message)
  }

  await page.setViewportSize({ width: 1440, height: 1000 })
  await login('#workspace-atlas')
  await page.locator('#assets-heading').waitFor()
  await axe('empty Atlas')

  const search = page.getByRole('searchbox', { name: 'Search Asset inventory' })
  await search.fill('phase-one-preserved-filter')
  await page.getByRole('link', { name: /^People —/ }).click()
  await page.locator('#people-heading').waitFor()
  await page.getByRole('link', { name: /^Atlas —/ }).click()
  assert(await search.inputValue() === 'phase-one-preserved-filter', 'Atlas filter was not preserved across workspace navigation')
  await page.getByRole('link', { name: /^People —/ }).click()
  await page.getByRole('tab', { name: 'Workflows & assignments' }).click()

  await page.getByRole('button', { name: 'Start person workflow' }).click()
  await assertFocused(page.getByRole('heading', { name: 'Step 1 — Person details' }), 'person step heading did not receive focus')
  await page.getByRole('button', { name: 'Continue to location' }).click()
  const validation = page.getByRole('alert')
  await validation.waitFor()
  assert((await validation.textContent()).includes('enter a display name'), 'person validation was not announced')
  await assertFocused(validation, 'person validation alert did not receive focus')

  const personName = 'Phase One Browser Person'
  const personEmail = 'phase-one-person@example.test'
  const siteName = 'Phase One Browser Site'
  await page.getByLabel('Person display name').fill(personName)
  await page.getByLabel('Person email address').fill(personEmail)
  await page.getByRole('button', { name: 'Continue to location' }).click()
  await assertFocused(page.getByRole('heading', { name: 'Step 2 — Choose or create a location' }), 'location step heading did not receive focus')
  await page.getByRole('button', { name: 'Back to person details' }).click()
  assert(await page.getByLabel('Person display name').inputValue() === personName, 'person name was lost after Back')
  assert(await page.getByLabel('Person email address').inputValue() === personEmail, 'person email was lost after Back')
  await page.getByRole('button', { name: 'Continue to location' }).click()
  await page.getByLabel('Create a missing location').check()
  await page.getByLabel('New location type').selectOption('site')
  await page.getByLabel('New site name').fill(siteName)
  await page.getByRole('button', { name: 'Create and review' }).click()
  await page.getByText(`Site — ${siteName}`, { exact: true }).waitFor()
  await page.getByRole('button', { name: 'Cancel workflow' }).click()
  await page.getByText(/Person workflow cancelled and its draft was cleared/).waitFor()
  await assertFocused(page.getByRole('button', { name: 'Start person workflow' }), 'workflow cancellation did not return focus')

  await page.getByRole('button', { name: 'Start person workflow' }).click()
  await page.getByLabel('Person display name').fill(personName)
  await page.getByLabel('Person email address').fill(personEmail)
  await page.getByRole('button', { name: 'Continue to location' }).click()
  await page.getByLabel('Existing location').selectOption({ label: `Site — ${siteName}` })
  await page.getByRole('button', { name: 'Continue to review' }).click()

  let controlledFailurePending = true
  const identityRoute = async route => {
    if (route.request().method() === 'POST' && controlledFailurePending) {
      controlledFailurePending = false
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        headers: { 'X-Correlation-ID': 'e2e-controlled-person-failure' },
        body: JSON.stringify({ error: { code: 'controlled_failure', message: 'Controlled temporary failure.' } }),
      })
      return
    }
    await route.continue()
  }
  await page.route('**/api/v1/identities', identityRoute)
  expectedConsoleErrors.people503 = 1
  await page.getByRole('button', { name: 'Create person' }).click()
  const failure = page.getByRole('alert')
  await failure.waitFor()
  assert((await failure.textContent()).includes('Controlled temporary failure'), 'recoverable People failure was not announced')
  await assertFocused(failure, 'recoverable People failure did not receive focus')
  assert(await page.getByText(personName, { exact: true }).count() > 0, 'People draft was lost after recoverable failure')
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await page.getByText(`${personName} was created at Site — ${siteName}.`, { exact: true }).waitFor()
  await page.unroute('**/api/v1/identities', identityRoute)
  await assertFocused(page.getByRole('button', { name: 'Start person workflow' }), 'successful workflow did not return focus')
  await axe('populated People')

  await page.getByRole('button', { name: 'Help for People' }).click()
  const guide = page.getByRole('dialog', { name: 'Guide help and walkthroughs' })
  await guide.waitFor()
  const docsHref = await guide.getByRole('link', { name: 'Read People documentation' }).getAttribute('href')
  const docsAreSameOrigin = await page.evaluate(href => {
    const link = window.document.createElement('a')
    link.href = href
    return link.protocol === window.location.protocol && link.host === window.location.host
  }, docsHref)
  assert(docsAreSameOrigin, 'People documentation link is not same-origin')
  await guide.getByRole('button', { name: 'Report issue' }).click()
  const reportHref = await guide.getByRole('link', { name: 'Review issue before submitting' }).getAttribute('href')
  const reportBody = await page.evaluate(href => {
    const link = window.document.createElement('a')
    link.href = href
    const query = new window.URLSearchParams(link.search)
    return query.get('body') || ''
  }, reportHref)
  for (const forbidden of [personName, personEmail, siteName, 'phase-one-preserved-filter', '#workspace-people', 'Administrator', 'csrf']) {
    assert(!reportBody.toLowerCase().includes(forbidden.toLowerCase()), `issue report leaked ${forbidden}`)
  }
  assert(reportBody.includes('Page: /'), 'issue report omitted its allow-listed page path')
  await axe('Guide report')
  await guide.getByRole('button', { name: 'Close Guide', exact: true }).click()

  await page.getByRole('link', { name: /^Guard —/ }).click()
  const guard = page.locator('section[aria-labelledby="guard-access-heading"]')
  await guard.waitFor()
  await guard.getByText('Create a custom role', { exact: true }).click()
  const roleForm = guard.locator('form').filter({ has: page.locator('#guardRoleName') })
  await roleForm.getByLabel('Role name', { exact: true }).fill('Phase One Reader')
  await roleForm.getByLabel('Description (optional)', { exact: true }).fill('Disposable phase-one browser read-only role.')
  for (const permission of ['organization.read', 'assets.read', 'directory.read']) {
    await roleForm.locator(`input[name="guardPermission"][value="${permission}"]`).check()
  }
  await roleForm.getByRole('button', { name: 'Create custom role', exact: true }).click()
  await guard.getByText('Custom role created and ready to assign.', { exact: true }).waitFor()
  await guard.getByText('Add a scoped role assignment', { exact: true }).click()
  const assignmentForm = guard.locator('form').filter({ has: page.locator('#guardAccountId') })
  await assignmentForm.getByLabel('Account', { exact: true }).selectOption({ label: 'Phase One Reader · phase-one-reader' })
  await assignmentForm.getByLabel('Role', { exact: true }).selectOption({ label: 'Phase One Reader' })
  await assignmentForm.getByLabel('Access scope', { exact: true }).selectOption('organization')
  await assignmentForm.getByRole('button', { name: 'Assign role', exact: true }).click()
  await guard.getByText('Scoped role assignment created.', { exact: true }).waitFor()
  await axe('Guard role assignment')

  await page.reload()
  await page.locator('#guard-access-heading').waitFor()
  assert(consumedPeople503 === 1 && expectedConsoleErrors.people503 === 0, 'controlled People 503 console budget was not consumed exactly once')
  assert(browserProblems.length === 0, `browser diagnostics: ${browserProblems.join(' | ')}`)
  return { scenario: 'workspace-admin', status: 'passed', axe: '0 violations' }
}
