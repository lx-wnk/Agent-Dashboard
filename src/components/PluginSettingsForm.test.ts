import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import PluginSettingsForm from './PluginSettingsForm.vue'

const schema = [
  { key: 'endpoint', type: 'url', label: 'Endpoint', secret: false },
  { key: 'apiKey', type: 'string', label: 'API Key', secret: true },
  { key: 'mode', type: 'enum', label: 'Mode', secret: false, enum: ['a', 'b'] },
]

function mountForm(getSettings: any, putSettings = vi.fn().mockResolvedValue(undefined)) {
  return mount(PluginSettingsForm, { props: { pluginId: 'p1', getSettings, putSettings } })
}

it('renders a control per field and masks secrets', async () => {
  const w = mountForm(async () => ({ schema, values: { endpoint: 'http://x', apiKey: '********', mode: 'a' } }))
  await flushPromises()
  expect(w.find('[data-field="endpoint"]').exists()).toBe(true)
  expect(w.find('select[data-field="mode"]').exists()).toBe(true)
  expect((w.find('[data-field="apiKey"]').element as HTMLInputElement).value).toBe('********')
})

it('put omits an untouched secret and sends changed fields', async () => {
  const put = vi.fn().mockResolvedValue(undefined)
  const w = mountForm(async () => ({ schema, values: { endpoint: 'http://x', apiKey: '********', mode: 'a' } }), put)
  await flushPromises()
  await w.find('[data-field="endpoint"]').setValue('http://y')
  await w.find('[data-action="save"]').trigger('click')
  await flushPromises()
  const sent = put.mock.calls[0][1]
  expect(sent.endpoint).toBe('http://y')
  expect('apiKey' in sent).toBe(false) // untouched secret omitted
})

const schemaWithInt = [
  { key: 'port', type: 'int', label: 'Port', secret: false },
  { key: 'name', type: 'string', label: 'Name', secret: false },
]

it('int field: edited value is sent as a string, not a number', async () => {
  const put = vi.fn().mockResolvedValue(undefined)
  const w = mountForm(async () => ({ schema: schemaWithInt, values: { port: '3000', name: 'x' } }), put)
  await flushPromises()
  await w.find('[data-field="port"]').setValue('8080')
  await w.find('[data-action="save"]').trigger('click')
  await flushPromises()
  const sent = put.mock.calls[0][1]
  expect(sent.port).toBe('8080')
  expect(typeof sent.port).toBe('string')
})

it('int field: untouched int is not re-sent on save', async () => {
  const put = vi.fn().mockResolvedValue(undefined)
  const w = mountForm(async () => ({ schema: schemaWithInt, values: { port: '3000', name: 'x' } }), put)
  await flushPromises()
  await w.find('[data-field="name"]').setValue('changed')
  await w.find('[data-action="save"]').trigger('click')
  await flushPromises()
  const sent = put.mock.calls[0][1]
  expect('port' in sent).toBe(false)
  expect(sent.name).toBe('changed')
})
