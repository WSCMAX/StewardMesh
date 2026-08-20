async page => {
  const assert = (condition, message) => {
    if (!condition) throw new Error(message)
  }
  const pathnameOf = value => {
    const withoutOrigin = value.replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]*/i, '')
    return withoutOrigin.split(/[?#]/)[0] || '/'
  }
  const browserProblems = []
  const expectedConsoleErrors = { association409: 0, label503: 0, resolve404: 0 }
  let consumedAssociation409 = 0
  let consumedLabel503 = 0
  let consumedResolve404 = 0
  page.on('console', message => {
    const text = message.text()
    const location = message.location().url
    if (message.type() === 'error' && expectedConsoleErrors.association409 > 0 &&
      text === 'Failed to load resource: the server responded with a status of 409 (Conflict)' &&
      /^http:\/\/127\.0\.0\.1:15173\/api\/v1\/assets\/[^/]+\/identifiers$/.test(location)) {
      expectedConsoleErrors.association409 -= 1
      consumedAssociation409 += 1
      return
    }
    if (message.type() === 'error' && expectedConsoleErrors.resolve404 > 0 &&
      text === 'Failed to load resource: the server responded with a status of 404 (Not Found)' &&
      location === 'http://127.0.0.1:15173/api/v1/asset-identifiers/resolve') {
      expectedConsoleErrors.resolve404 -= 1
      consumedResolve404 += 1
      return
    }
    if (message.type() === 'error' && expectedConsoleErrors.label503 > 0 &&
      text === 'Failed to load resource: the server responded with a status of 503 (Service Unavailable)' &&
      location === 'http://127.0.0.1:15173/api/v1/asset-label-batches') {
      expectedConsoleErrors.label503 -= 1
      consumedLabel503 += 1
      return
    }
    if (message.type() === 'warning' || message.type() === 'error') browserProblems.push(`${message.type()}: ${text}`)
  })
  page.on('pageerror', error => browserProblems.push(`pageerror: ${error.message}`))

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('http://127.0.0.1:15173/#workspace-atlas')
  await page.getByRole('heading', { name: 'Sign in to StewardMesh' }).waitFor()
  await page.getByLabel('Username').fill('phase-one-admin')
  await page.getByLabel('Password', { exact: true }).fill('Phase-one-admin-password!')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await page.locator('#assets-heading').waitFor()

  const openAtlasTab = async name => {
    await page.getByRole('tab', { name, exact: true }).click()
  }
  const dismissDrawer = async () => {
    const dialog = page.getByRole('dialog')
    if (await dialog.count() === 0) return
    await page.keyboard.press('Escape')
    await dialog.first().waitFor({ state: 'hidden' })
  }
  const createAsset = async (name, tag, serial) => {
    await openAtlasTab('Assets')
    await dismissDrawer()
    await page.getByRole('button', { name: 'Add asset' }).click()
    const form = page.getByRole('form', { name: 'Add asset' })
    await form.getByLabel('Asset name').fill(name)
    await form.getByLabel('Asset tag').fill(tag)
    await form.getByLabel('Serial number').fill(serial)
    await form.getByLabel('Status').selectOption('active')
    await form.getByRole('button', { name: 'Create asset' }).click()
    await page.getByText('Asset created.', { exact: true }).waitFor()
    await page.locator('#asset-identifiers-heading').waitFor()
    await dismissDrawer()
  }
  const selectAsset = async name => {
    await openAtlasTab('Assets')
    await dismissDrawer()
    await page.getByRole('button', { name: `Open ${name}` }).click()
    await page.locator('#asset-identifiers-heading').waitFor()
    await page.getByText(name, { exact: true }).last().waitFor()
    await dismissDrawer()
  }
  const scannerPanel = page.locator('section[aria-labelledby="atlas-scanner-heading"]')
  const scannerForm = () => scannerPanel.getByRole('form', { name: 'Scan an Atlas Code', exact: true })
  const scannerSelect = label => scannerForm().locator('label').filter({ hasText: new RegExp(`^${label}`) }).locator('select')
  const scannerInput = () => scannerForm().locator('input[placeholder="Scan, paste, or type"]')
  const setScanner = async (mode, symbology = 'code128') => {
    await dismissDrawer()
    await openAtlasTab('Scan')
    if (await scannerPanel.getByRole('button', { name: 'Open scanner', exact: true }).count()) await scannerPanel.getByRole('button', { name: 'Open scanner', exact: true }).click()
    await scannerSelect('Workflow').selectOption(mode)
    await scannerSelect('Symbology').selectOption(symbology)
  }
  const associate = async (value, terminator = 'Enter') => {
    await scannerInput().fill(value)
    await scannerInput().press(terminator)
  }

  const assetA = 'Phase One Asset A'
  // QR labels reserve most of their 50 mm width for the code. Keep the
  // human-readable fixture inside the renderer's physical fit budget; the
  // focused Go suite separately proves that oversized labels are rejected.
  const assetB = 'P1 Asset B'
  const codeA = 'E2E-CODE-A'
  const codeAReplacement = 'E2E-CODE-A-R2'
  const codeB = 'E2E-CODE-B'
  const qrA = 'https://e2e.invalid/atlas/a'
  const qrB = 'QR-B'
  await createAsset(assetA, 'E2E-A', 'E2E-PRIVATE-SERIAL-A')
  await createAsset(assetB, 'E2E-B', 'E2E-PRIVATE-SERIAL-B')

  let associationRequests = 0
  page.on('request', request => {
    if (request.method() === 'POST' && /^\/api\/v1\/assets\/[^/]+\/identifiers$/.test(pathnameOf(request.url()))) associationRequests += 1
  })

  await selectAsset(assetA)
  await setScanner('associate', 'code128')
  await associate(codeA)
  await scannerPanel.getByText(`Identifier associated with ${assetA}.`, { exact: true }).waitFor()
  const requestsAfterFirstAssociation = associationRequests
  await associate(codeA)
  await scannerPanel.getByText('Duplicate scan ignored. The first scan already completed.', { exact: true }).waitFor()
  assert(associationRequests === requestsAfterFirstAssociation, 'duplicate scan reached the association API')

  await scannerSelect('Symbology').selectOption('qr')
  await scannerSelect('Scanner terminator').selectOption('Tab')
  await scannerInput().click()
  await page.evaluate(async value => navigator.clipboard.writeText(value), qrA)
  await scannerInput().press('ControlOrMeta+V')
  await scannerInput().press('Tab')
  await scannerPanel.getByText(`Identifier associated with ${assetA}.`, { exact: true }).waitFor()

  const requestsBeforeInvalid = associationRequests
  await scannerSelect('Symbology').selectOption('code128')
  await scannerInput().fill('X'.repeat(129))
  await scannerForm().getByRole('button', { name: 'Associate identifier', exact: true }).click()
  await scannerPanel.getByRole('alert').filter({ hasText: 'Code 128 values must be 1–128 printable ASCII characters.' }).waitFor()
  await scannerSelect('Symbology').selectOption('qr')
  await scannerInput().fill('é'.repeat(300))
  await scannerForm().getByRole('button', { name: 'Associate identifier', exact: true }).click()
  await scannerPanel.getByRole('alert').filter({ hasText: 'QR values must be control-free UTF-8 no longer than 512 bytes.' }).waitFor()
  assert(associationRequests === requestsBeforeInvalid, 'invalid scanner input reached the association API')

  await selectAsset(assetB)
  await openAtlasTab('Scan')
  await scannerSelect('Symbology').selectOption('code128')
  await scannerSelect('Scanner terminator').selectOption('Enter')
  await scannerSelect('Scanner burst window').selectOption('250')
  const slowCode = 'E2E-SLOW-MANUAL'
  await scannerInput().fill('')
  await scannerInput().pressSequentially(slowCode, { delay: 35 })
  await scannerInput().press('Enter')
  await scannerPanel.getByRole('alert').filter({ hasText: 'input exceeded the 250 ms burst window' }).waitFor()
  assert(await scannerInput().inputValue() === slowCode, 'slow wedge value was not retained for manual review')
  const requestsBeforeManual = associationRequests
  await scannerForm().getByRole('button', { name: 'Associate identifier', exact: true }).click()
  await scannerPanel.getByText(`Identifier associated with ${assetB}.`, { exact: true }).waitFor()
  assert(associationRequests === requestsBeforeManual + 1, 'manual scanner fallback did not make one association request')
  await scannerSelect('Scanner burst window').selectOption('500')
  await associate(codeB)
  await scannerPanel.getByText(`Identifier associated with ${assetB}.`, { exact: true }).waitFor()
  await scannerSelect('Symbology').selectOption('qr')
  await associate(qrB)
  await scannerPanel.getByText(`Identifier associated with ${assetB}.`, { exact: true }).waitFor()

  await page.waitForTimeout(1600)
  await scannerSelect('Symbology').selectOption('code128')
  expectedConsoleErrors.association409 = 1
  await associate(codeA)
  const conflict = scannerPanel.getByRole('alert').filter({ hasText: 'the identifier association or revision conflicts with current data' })
  await conflict.waitFor()
  assert(await scannerInput().inputValue() === codeA, 'conflicting code was not retained')
  assert(!(await conflict.textContent()).includes(assetA), 'identifier conflict disclosed the owning asset')

  const requestsBeforeClosedInput = associationRequests
  await scannerPanel.getByRole('button', { name: 'Cancel scanning', exact: true }).click()
  await page.keyboard.type('E2E-BACKGROUND-CODE')
  await page.keyboard.press('Enter')
  assert(associationRequests === requestsBeforeClosedInput, 'background keyboard input mutated Atlas while scanner was closed')

  await setScanner('find', 'code128')
  await associate(codeA)
  await scannerPanel.getByText('Identifier matched. The authorized asset is shown below.', { exact: true }).waitFor()
  await page.locator('#atlas-panel-scan').getByText(assetA, { exact: true }).waitFor()
  await scannerInput().fill('E2E-CODE-MISSING')
  expectedConsoleErrors.resolve404 = 1
  await scannerForm().getByRole('button', { name: 'Find asset', exact: true }).click()
  await scannerForm().getByRole('button', { name: 'Retry scan', exact: true }).waitFor()
  assert(await scannerInput().inputValue() === 'E2E-CODE-MISSING', 'failed find did not retain its code')
  await scannerPanel.getByRole('button', { name: 'Cancel scanning', exact: true }).click()
  await scannerPanel.getByText('Scanning cancelled. No asset or identifier was changed.', { exact: true }).waitFor()

  await openAtlasTab('Assets')
  await page.getByRole('button', { name: `Open ${assetA}` }).click()
  const identifiers = page.locator('section[aria-labelledby="asset-identifiers-heading"]')
  const codeAItem = identifiers.locator('li').filter({ has: page.getByText(codeA, { exact: true }) })
  await codeAItem.getByRole('button', { name: 'Replace' }).click()
  const replaceForm = identifiers.getByRole('form', { name: `Replace ${codeA}` })
  await replaceForm.getByLabel('Encoded value').fill(codeAReplacement)
  await replaceForm.getByLabel('Display value').fill(codeAReplacement)
  await replaceForm.getByRole('button', { name: 'Confirm replacement' }).click()
  await codeAItem.getByText(/Code 128 · replaced/).waitFor()
  assert((await codeAItem.textContent()).includes('replaced'), 'replacement removed the previous identifier history')
  const replacementItem = identifiers.locator('li').filter({ has: page.getByText(codeAReplacement, { exact: true }) })
  await replacementItem.getByText(/Code 128 · active/).waitFor()
  assert((await replacementItem.textContent()).includes('active'), 'replacement successor is not active')
  const qrAItem = identifiers.locator('li').filter({ hasText: qrA })
  await qrAItem.getByRole('button', { name: 'Deactivate' }).click()
  await qrAItem.getByRole('button', { name: 'Confirm deactivation' }).click()
  await qrAItem.getByText(/QR · deactivated/).waitFor()
  assert((await qrAItem.textContent()).includes('deactivated'), 'deactivation removed identifier history')

  const cameraRequests = []
  page.on('request', request => {
    if (request.method() === 'POST' && request.url().includes('/api/')) cameraRequests.push({ url: request.url(), body: request.postData() || '' })
  })
  await page.evaluate(cameraValue => {
    const state = { calls: 0, constraints: null, stops: 0, delivered: false }
    globalThis.__e2eCamera = state
    const canvas = document.createElement('canvas')
    const stream = canvas.captureStream()
    for (const track of stream.getTracks()) {
      const originalStop = track.stop.bind(track)
      track.stop = () => { state.stops += 1; originalStop() }
    }
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async constraints => { state.calls += 1; state.constraints = constraints; return stream } },
    })
    Object.defineProperty(globalThis, 'BarcodeDetector', {
      configurable: true,
      value: class {
        async detect() {
          if (state.delivered) return []
          state.delivered = true
          return [{ format: 'code_128', rawValue: cameraValue }]
        }
      },
    })
    HTMLMediaElement.prototype.play = async () => undefined
  }, codeAReplacement)
  await setScanner('find', 'code128')
  assert(await page.evaluate(() => globalThis.__e2eCamera.calls) === 0, 'camera permission was requested before explicit activation')
  await scannerForm().getByRole('button', { name: 'Use camera', exact: true }).click()
  await scannerPanel.getByText('Identifier matched. The authorized asset is shown below.', { exact: true }).waitFor()
  const cameraState = await page.evaluate(() => globalThis.__e2eCamera)
  assert(cameraState.calls === 1, `camera requested ${cameraState.calls} times`)
  assert(cameraState.constraints.audio === false && cameraState.constraints.video.facingMode.ideal === 'environment', 'camera constraints were not narrow and rear-facing')
  assert(cameraState.stops >= 1, 'camera track was not stopped after capture')
  assert(cameraRequests.length === 1 && cameraRequests.every(request => pathnameOf(request.url) === '/api/v1/asset-identifiers/resolve' && !request.body.includes('data:') && !request.body.includes('frame')), 'camera flow did not make exactly one metadata-only resolve request')
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia: async () => { throw new Error('denied') } } })
    globalThis.__e2eCamera.delivered = false
  })
  await scannerForm().getByRole('button', { name: 'Use camera', exact: true }).click()
  await scannerPanel.getByRole('alert').filter({ hasText: 'Camera access was denied or unavailable.' }).waitFor()
  await page.evaluate(() => { delete globalThis.BarcodeDetector })
  await scannerForm().getByRole('button', { name: 'Use camera', exact: true }).click()
  await scannerPanel.getByRole('alert').filter({ hasText: 'Camera scanning is not available in this browser.' }).waitFor()
  await scannerPanel.getByRole('button', { name: 'Cancel scanning', exact: true }).click()

  await openAtlasTab('Labels')
  await dismissDrawer()
  await page.getByRole('button', { name: 'Print labels' }).click()
  const labelPanel = page.locator('section[aria-labelledby="atlas-label-print-heading"]')
  await labelPanel.getByText('1. Select active identifiers', { exact: true }).waitFor()
  const inspectSVGPreview = async () => {
    const image = labelPanel.locator('img[alt^="Generated preview of"]')
    await image.waitFor()
    return image.evaluate(async element => {
      const dataURI = element.getAttribute('src') || ''
      const comma = dataURI.indexOf(',')
      if (!dataURI.startsWith('data:image/svg+xml') || comma < 0) return { dataURI: false, element: '', width: '', height: '', viewBox: '', sha256: '', privateLeak: true }
      const metadata = dataURI.slice(0, comma)
      const payload = dataURI.slice(comma + 1)
      const source = /;base64/i.test(metadata)
        ? new TextDecoder().decode(Uint8Array.from(atob(payload), character => character.charCodeAt(0)))
        : decodeURIComponent(payload)
      const root = new DOMParser().parseFromString(source, 'image/svg+xml').documentElement
      const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(source))
      return {
        dataURI: true,
        element: root.localName,
        width: root.getAttribute('width') || '',
        height: root.getAttribute('height') || '',
        viewBox: root.getAttribute('viewBox') || '',
        sha256: Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join(''),
        privateLeak: source.includes('E2E-PRIVATE-SERIAL') || source.includes('phase-one-admin@example.test'),
      }
    })
  }
  const labelChoices = labelPanel.getByRole('group', { name: '1. Select active identifiers' })
  const labelChoice = value => labelChoices.locator('label').filter({ hasText: value }).getByRole('checkbox')
  for (const checkbox of await labelChoices.getByRole('checkbox').all()) {
    if (await checkbox.isChecked()) await checkbox.uncheck()
  }
  await labelChoice(codeAReplacement).check()
  await labelPanel.getByLabel('Output path').selectOption('svg')
  await page.evaluate(() => { globalThis.__e2ePrintCalls = 0; globalThis.print = () => { globalThis.__e2ePrintCalls += 1 } })
  const svgResponsePromise = page.waitForResponse(response => response.url().includes('/api/v1/asset-label-batches') && response.request().method() === 'POST')
  await labelPanel.getByRole('button', { name: 'Generate test-print preview' }).click()
  const svgResponse = await svgResponsePromise
  assert(svgResponse.status() === 200, `SVG label returned ${svgResponse.status()}`)
  assert(svgResponse.headers()['content-type'].startsWith('image/svg+xml'), 'SVG label content type is wrong')
  assert(svgResponse.headers()['x-label-width-mm'] === '70.00' && svgResponse.headers()['x-label-height-mm'] === '30.00', 'Code 128 label dimensions are wrong')
  const svgArtifact = await inspectSVGPreview()
  assert(svgArtifact.dataURI && svgArtifact.element === 'svg' && svgArtifact.width === '70.00mm' && svgArtifact.height === '30.00mm' && svgArtifact.viewBox === '0 0 70.00 30.00', `SVG physical dimensions are invalid: ${JSON.stringify(svgArtifact)}`)
  assert(!svgArtifact.privateLeak, 'SVG leaked a private asset or account field')
  const printButton = labelPanel.getByRole('button', { name: 'Open browser print dialog' })
  assert(await printButton.isDisabled(), 'SVG print was enabled before operator review')
  assert(await page.evaluate(() => globalThis.__e2ePrintCalls) === 0, 'application printed silently')
  await labelPanel.getByLabel(/I reviewed the 70 × 30 mm dimensions/).check()
  await printButton.click()
  assert(await page.evaluate(() => globalThis.__e2ePrintCalls) === 1, 'operator-confirmed SVG print did not open exactly once')

  for (const checkbox of await labelChoices.getByRole('checkbox').all()) {
    if (await checkbox.isChecked()) await checkbox.uncheck()
  }
  await labelChoice(codeAReplacement).check()
  await labelChoice(codeB).check()
  assert(await labelPanel.getByLabel('Output path').inputValue() === 'pdf', 'multi-label selection did not switch to PDF')
  await page.evaluate(() => {
    globalThis.__e2eOpenCalls = 0
    globalThis.__e2eOpenedURL = ''
    globalThis.open = url => { globalThis.__e2eOpenCalls += 1; globalThis.__e2eOpenedURL = String(url); return null }
  })
  const pdfResponsePromise = page.waitForResponse(response => response.url().includes('/api/v1/asset-label-batches') && response.request().method() === 'POST')
  await labelPanel.getByRole('button', { name: 'Generate test-print preview' }).click()
  const pdfResponse = await pdfResponsePromise
  assert(pdfResponse.status() === 200, `PDF label returned ${pdfResponse.status()}`)
  assert(pdfResponse.headers()['x-label-item-count'] === '2', 'PDF label count header is wrong')
  await labelPanel.getByRole('heading', { name: '3. Review and confirm' }).waitFor()
  const pdfButton = labelPanel.getByRole('button', { name: 'Open PDF for printing' })
  assert(await pdfButton.isDisabled() && await page.evaluate(() => globalThis.__e2eOpenCalls) === 0, 'PDF viewer opened before operator review')
  await labelPanel.getByLabel(/I reviewed the 70 × 30 mm dimensions/).check()
  await pdfButton.click()
  assert(await page.evaluate(() => globalThis.__e2eOpenCalls) === 1, 'operator-confirmed PDF did not open exactly once')
  const pdfArtifact = await page.evaluate(async () => {
    const openedURL = String(globalThis.__e2eOpenedURL || '')
    if (!openedURL.startsWith('blob:')) return { blobURL: false, status: 0, prefix: '', pages: 0, privateLeak: true }
    const response = await fetch(openedURL)
    const text = new TextDecoder('latin1').decode(await response.arrayBuffer())
    return {
      blobURL: true,
      status: response.status,
      prefix: text.slice(0, 8),
      pages: (text.match(/\/Type \/Page\b/g) || []).length,
      privateLeak: text.includes('E2E-PRIVATE-SERIAL') || text.includes('phase-one-admin@example.test'),
    }
  })
  assert(pdfArtifact.blobURL && pdfArtifact.status === 200 && pdfArtifact.prefix.startsWith('%PDF-1.4'), `PDF label artifact is invalid: ${JSON.stringify(pdfArtifact)}`)
  assert(pdfArtifact.pages === 2, `PDF label batch has ${pdfArtifact.pages} pages instead of 2`)
  assert(!pdfArtifact.privateLeak, 'PDF leaked a private asset or account field')
  await labelPanel.getByText(/If the browser blocked it, allow the new tab and retry/).waitFor()

  for (const checkbox of await labelChoices.getByRole('checkbox').all()) {
    if (await checkbox.isChecked()) await checkbox.uncheck()
  }
  await labelChoice(qrB).check()
  await labelPanel.getByLabel('Output path').selectOption('svg')
  const qrTemplateSelect = labelPanel.getByLabel('Versioned label template')
  const qrTemplateOption = qrTemplateSelect.locator('option:checked').filter({ hasText: 'Atlas QR label · v1 · 50 × 30 mm' })
  await labelPanel.getByText('50 × 30 mm · 3 mm margins', { exact: true }).waitFor()
  assert(await qrTemplateSelect.inputValue() === 'builtin-atlas-label-qr', `QR template selection is wrong: ${JSON.stringify(await qrTemplateSelect.inputValue())}`)
  assert(await qrTemplateOption.textContent() === 'Atlas QR label · v1 · 50 × 30 mm', `QR template option text is wrong: ${JSON.stringify(await qrTemplateOption.textContent())}`)
  const qrResponsePromise = page.waitForResponse(response => response.url().includes('/api/v1/asset-label-batches') && response.request().method() === 'POST')
  await labelPanel.getByRole('button', { name: 'Generate test-print preview' }).click()
  const qrResponse = await qrResponsePromise
  const qrHeaders = qrResponse.headers()
  const qrContentType = qrHeaders['content-type'] || ''
  const qrWidth = qrHeaders['x-label-width-mm'] || ''
  const qrHeight = qrHeaders['x-label-height-mm'] || ''
  assert(qrResponse.status() === 200, `QR label returned ${qrResponse.status()}`)
  assert(qrContentType.startsWith('image/svg+xml'), `QR label content type is wrong: ${JSON.stringify(qrContentType)}`)
  assert(qrWidth === '50.00' && qrHeight === '30.00', `QR label dimensions are wrong: width=${JSON.stringify(qrWidth)}, height=${JSON.stringify(qrHeight)}`)
  const qrArtifact = await inspectSVGPreview()
  assert(qrArtifact.dataURI && qrArtifact.element === 'svg' && qrArtifact.width === '50.00mm' && qrArtifact.height === '30.00mm' && qrArtifact.viewBox === '0 0 50.00 30.00', `QR SVG physical dimensions are invalid: ${JSON.stringify(qrArtifact)}`)
  const qrTemplateMetadata = await page.evaluate(encoded => {
    const base64 = encoded.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(encoded.length / 4) * 4, '=')
    return JSON.parse(atob(base64))
  }, qrResponse.headers()['x-stewardmesh-label-template'])
  assert(qrTemplateMetadata.payloadSource === 'organization_route', 'QR template does not encode an opaque organization route')
  assert(!qrArtifact.privateLeak, 'QR label leaked a private asset or account field')
  assert(await labelPanel.getByLabel('Output path').locator('option[value="zpl"]').count() === 0, 'UI advertises an unsupported ZPL output')

  let releaseCancelledRequest
  const cancelledRequestReleased = new Promise(resolve => { releaseCancelledRequest = resolve })
  let cancelledRequestObserved = false
  const cancelRoute = async route => {
    cancelledRequestObserved = true
    await cancelledRequestReleased
    try {
      await route.abort('aborted')
    } catch {
      // The browser-side AbortController is expected to win this race.
    }
  }
  await page.route('**/api/v1/asset-label-batches', cancelRoute)
  const cancelledFailurePromise = page.waitForEvent('requestfailed', {
    predicate: request => request.method() === 'POST' && request.url().includes('/api/v1/asset-label-batches'),
  })
  await labelPanel.getByRole('button', { name: 'Regenerate preview' }).click()
  await labelPanel.getByRole('button', { name: 'Cancel generation' }).click()
  await labelPanel.getByText('Label generation cancelled. Retry is safe and uses the same request key.', { exact: true }).waitFor()
  releaseCancelledRequest()
  const cancelledRequest = await cancelledFailurePromise
  await page.unroute('**/api/v1/asset-label-batches', cancelRoute)
  const cancellationFailure = cancelledRequest.failure()
  const cancellationText = typeof cancellationFailure === 'string' ? cancellationFailure : cancellationFailure?.errorText || ''
  assert(cancelledRequestObserved, 'cancellable label request was not started')
  assert(/abort/i.test(cancellationText), `label request was not aborted: ${cancellationText}`)
  assert(await labelPanel.getByRole('heading', { name: '3. Review and confirm' }).count() === 0, 'cancelled label response produced a stale preview')
  assert(await labelPanel.getByRole('button', { name: /Open .* for printing|Open browser print dialog/ }).count() === 0, 'cancelled label response enabled output')
  await labelPanel.getByRole('button', { name: 'Generate test-print preview' }).click()
  await labelPanel.getByRole('heading', { name: '3. Review and confirm' }).waitFor()

  let failNextLabel = true
  const retryKeys = []
  const retryRoute = async route => {
    retryKeys.push(route.request().headers()['idempotency-key'])
    if (failNextLabel) {
      failNextLabel = false
      await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { message: 'Controlled label failure.' } }) })
      return
    }
    await route.continue()
  }
  await page.route('**/api/v1/asset-label-batches', retryRoute)
  expectedConsoleErrors.label503 = 1
  await labelPanel.getByRole('button', { name: 'Regenerate preview' }).click()
  await labelPanel.getByRole('alert').filter({ hasText: 'Controlled label failure.' }).waitFor()
  const retryResponsePromise = page.waitForResponse(response => response.url().includes('/api/v1/asset-label-batches') && response.request().method() === 'POST')
  await labelPanel.getByRole('button', { name: 'Retry generation' }).click()
  const retryResponse = await retryResponsePromise
  await labelPanel.getByRole('heading', { name: '3. Review and confirm' }).waitFor()
  const retryArtifact = await inspectSVGPreview()
  await page.unroute('**/api/v1/asset-label-batches', retryRoute)
  assert(retryKeys.length === 2 && retryKeys[0] && retryKeys[0] === retryKeys[1], 'label retry changed the idempotency key')
  assert(retryResponse.status() === 200 && retryResponse.headers()['x-idempotent-replay'] === 'true', 'label retry was not an exact replay')
  assert(retryArtifact.sha256 && retryArtifact.sha256 === qrArtifact.sha256, 'label retry changed the app-held artifact bytes')
  await labelPanel.getByRole('button', { name: 'Regenerate preview' }).click()
  await labelPanel.getByText(/safe retry replay/).waitFor()

  await page.addScriptTag({ path: 'web/node_modules/axe-core/axe.min.js' })
  const violations = await page.evaluate(async () => (await globalThis.axe.run(document)).violations.map(violation => `${violation.id}:${violation.nodes.length}`))
  assert(violations.length === 0, `Atlas Codes axe violations: ${violations.join(', ')}`)
  await page.setViewportSize({ width: 320, height: 900 })
  const width = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }))
  assert(width.scroll <= width.client, `Atlas Codes mobile overflowed: ${width.scroll} > ${width.client}`)
  assert(consumedAssociation409 === 1 && expectedConsoleErrors.association409 === 0, 'controlled cross-asset 409 console budget was not consumed exactly once')
  assert(consumedResolve404 === 1 && expectedConsoleErrors.resolve404 === 0, 'controlled unknown-code 404 console budget was not consumed exactly once')
  assert(consumedLabel503 === 1 && expectedConsoleErrors.label503 === 0, 'controlled label 503 console budget was not consumed exactly once')
  assert(browserProblems.length === 0, `browser diagnostics: ${browserProblems.join(' | ')}`)
  return { scenario: 'atlas-codes-admin', status: 'passed', svg: '70x30', pdfPages: 2, qr: '50x30', axe: '0 violations' }
}
