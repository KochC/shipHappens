import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import Counter from './Counter.vue'

describe('Counter', () => {
  it('renders initial state', () => {
    const wrapper = mount(Counter)
    expect(wrapper.find('.count').text()).toContain('count: 0')
    expect(wrapper.find('.doubled').text()).toContain('doubled: 0')
  })

  it('increments and doubles on click', async () => {
    const wrapper = mount(Counter)
    await wrapper.find('button').trigger('click')
    await wrapper.find('button').trigger('click')
    expect(wrapper.find('.count').text()).toContain('count: 2')
    expect(wrapper.find('.doubled').text()).toContain('doubled: 4')
  })
})
