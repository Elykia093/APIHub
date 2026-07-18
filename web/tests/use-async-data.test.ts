import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent, type Ref } from 'vue';
import { describe, expect, it } from 'vitest';
import { useAsyncData } from '@/composables/useAsyncData';

describe('useAsyncData', () => {
  it('does not let an aborted request overwrite a newer result', async () => {
    let invocation = 0;
    let state: ReturnType<typeof useAsyncData<number>> | undefined;
    const component = defineComponent({
      setup() {
        state = useAsyncData(0, (signal) => {
          invocation += 1;
          if (invocation === 2) return Promise.resolve(2);
          return new Promise<number>((_resolve, reject) => {
            signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
          });
        });
        return { value: state.data as Ref<number> };
      },
      template: '<span>{{ value }}</span>',
    });
    const wrapper = mount(component);

    const first = state!.load();
    const second = state!.load();
    await Promise.all([first, second]);
    await flushPromises();

    expect(state!.data.value).toBe(2);
    expect(state!.error.value).toBe('');
    expect(wrapper.text()).toBe('2');
  });
});
