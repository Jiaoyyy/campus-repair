import { useLayoutEffect, useState } from 'react';

export function readView<T>(key: string | undefined, fallback: T): T {
 try {
  const raw = key && sessionStorage.getItem(key);
  return raw ? JSON.parse(raw) ?? fallback : fallback;
 } catch { return fallback; }
}

export function saveView<T>(key: string | undefined, value: T) {
 if (!key) return;
 try { sessionStorage.setItem(key, JSON.stringify(value)); } catch { /* Keep the live page usable when storage is full. */ }
}

// The caller is keyed by its source message, so each mounted card owns one key.
export function useCardView<T>(key: string | undefined, field: string, initial: T) {
 const path = key && key + '/' + field;
 const [value, setValue] = useState<T>(() => readView(path, initial));
 useLayoutEffect(() => { saveView(path, value); }, [path, value]);
 return [value, setValue] as const;
}
