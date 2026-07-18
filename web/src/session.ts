import { ref } from 'vue';

const storageKey = 'apihub-admin-token';
const tokenState = ref(sessionStorage.getItem(storageKey) ?? '');

export const hasSession = () => tokenState.value.length > 0;
export const sessionToken = () => tokenState.value;

export function setSession(token: string): void {
  tokenState.value = token;
  sessionStorage.setItem(storageKey, token);
}

export function clearSession(): void {
  tokenState.value = '';
  sessionStorage.removeItem(storageKey);
}
