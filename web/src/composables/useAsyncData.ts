import { onBeforeUnmount, ref, type Ref } from 'vue';
import { APIError } from '@/api';

export function useAsyncData<T>(initial: T, loader: (signal: AbortSignal) => Promise<T>) {
  const data = ref(initial) as Ref<T>;
  const loading = ref(false);
  const error = ref('');
  let controller: AbortController | undefined;
  let requestSequence = 0;

  async function load(): Promise<void> {
    controller?.abort();
    const activeController = new AbortController();
    controller = activeController;
    const sequence = ++requestSequence;
    loading.value = true;
    error.value = '';
    try {
      const result = await loader(activeController.signal);
      if (sequence === requestSequence) data.value = result;
    } catch (caught) {
      if (activeController.signal.aborted || sequence !== requestSequence) return;
      error.value = caught instanceof APIError ? caught.message : '加载失败，请稍后重试';
    } finally {
      if (sequence === requestSequence) loading.value = false;
    }
  }

  onBeforeUnmount(() => controller?.abort());
  return { data, loading, error, load };
}
