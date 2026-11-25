import { useEffect, useId, useRef, useState } from 'react';
import type { ChangeEvent } from 'react';
import { api, tokenKey } from './api';
import { Icon } from './ui';
import './order-media.css';

const mib = 1024 * 1024;
const photoLimit = 25 * mib;
const videoLimit = 20 * mib;
const photoCount = 6;
const photoTypes = new Set(['image/jpeg', 'image/png']);
const videoTypes = new Set(['video/mp4', 'video/quicktime']);
export type SelectedMedia = { file: File; kind: 'photo' | 'video' };
type MediaEntry = { id: number; kind: SelectedMedia['kind']; content_type: string; size: number };
type PreviewPhoto = { url: string; name: string; opener: HTMLButtonElement };

function selectedKind(file: File): SelectedMedia['kind'] | undefined {
  if (photoTypes.has(file.type) || /\.(jpe?g|png)$/i.test(file.name)) return 'photo';
  if (videoTypes.has(file.type) || /\.(mp4|mov)$/i.test(file.name)) return 'video';
}

async function compressPhoto(file: File): Promise<Blob> {
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' });
  try {
    const scale = Math.min(1, 2560 / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const surface = canvas.getContext('2d', { alpha: false });
    if (!surface) return file;
    surface.fillStyle = '#fff';
    surface.fillRect(0, 0, canvas.width, canvas.height);
    surface.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const encoded = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, 'image/jpeg', 0.86));
    return encoded && encoded.size < file.size ? encoded : file;
  } finally {
    bitmap.close();
  }
}

// Reuse encoded bytes when the same submission is retried.
const photoCache = new WeakMap<File, Promise<Blob>>();
let compressionQueue: Promise<unknown> = Promise.resolve();

function preparedPhoto(file: File) {
  const previous = photoCache.get(file);
  if (previous) return previous;
  const pending = compressionQueue.then(() => compressPhoto(file)).catch(() => file);
  photoCache.set(file, pending);
  compressionQueue = pending;
  return pending;
}

export async function orderMediaBody(input: Record<string, string>, files: SelectedMedia[], onCompress: (position: number, count: number) => void) {
  const payload = new FormData();
  Object.entries(input).forEach(([name, value]) => payload.append(name, value));
  const photos = files.filter(item => item.kind === 'photo');
  let position = 0;
  let bytes = 0;
  for (const { file, kind } of files) {
    let blob: Blob = file;
    if (kind === 'photo') {
      onCompress(++position, photos.length);
      blob = await preparedPhoto(file);
    }
    bytes += blob.size;
    payload.append(kind, blob, kind === 'photo' && blob !== file ? 'photo.jpg' : file.name);
  }
  if (bytes > 80 * mib) throw new Error('附件合计超过 80 MB，请移除部分文件后重试。');
  return payload;
}

function SelectedPhoto({ file, index }: { file: File; index: number }) {
  const [src, setSrc] = useState('');
  useEffect(() => {
    let active = true;
    let url: string | undefined;
    void preparedPhoto(file).then(prepared => {
      if (!active || prepared.size > 5 * mib) return;
      url = URL.createObjectURL(prepared);
      setSrc(url);
    });
    return () => { active = false; if (url) URL.revokeObjectURL(url); };
  }, [file]);
  const [failed, setFailed] = useState(false);
  return src && !failed ? <img src={src} alt={`所选照片 ${index}`} onError={() => setFailed(true)} /> : <Icon kind="file" />;
}

export function OrderMediaInput({ files, onChange, status, draft = false }: { draft?: boolean; files: SelectedMedia[]; onChange: (files: SelectedMedia[]) => void; status: string }) {
  const [error, setError] = useState('');
  const titleId = useId();
  const helpId = useId();
  const picker = useRef<HTMLInputElement>(null);

  function select(event: ChangeEvent<HTMLInputElement>) {
    const next = [...files];
    const issues: string[] = [];
    for (const file of event.target.files ?? []) {
      const kind = selectedKind(file);
      if (!kind) { issues.push('请选择 JPEG、PNG、MP4 或 MOV 文件。'); continue; }
      if (next.some(item => item.file.name === file.name && item.file.size === file.size && item.file.lastModified === file.lastModified)) continue;
      if (!file.size || file.size > (kind === 'photo' ? photoLimit : videoLimit)) {
        issues.push(kind === 'photo' ? '单张原图须小于或等于 25 MB。' : '单段视频须小于或等于 20 MB。');
        continue;
      }
      const count = next.filter(item => item.kind === kind).length;
      if (count >= (kind === 'photo' ? photoCount : 1)) {
        issues.push(kind === 'photo' ? '最多选择 6 张照片。' : '最多选择 1 段视频。');
        continue;
      }
      next.push({ file, kind });
    }
    if (next.length !== files.length) onChange(next);
    setError([...new Set(issues)].join(' '));
    event.target.value = '';
  }

  return <section className="field-section order-media-input" aria-labelledby={titleId}>
    <h2 id={titleId}>{!draft && <span className="step-number">4</span>}现场附件 <small>选填</small></h2>
    <p className="field-help" id={helpId}>最多 6 张 JPEG 或 PNG，原图每张 25 MB，上传前自动压缩。可附 1 段 MP4 或 MOV，最长 15 秒、最大 20 MB。{draft && '附件仅在本页保留，确认提交报修时上传。'}</p>
    <input ref={picker} type="file" aria-label="选择现场照片或视频" accept="image/jpeg,image/png,video/mp4,video/quicktime,.jpg,.jpeg,.png,.mp4,.mov" multiple hidden onChange={select} />
    <button type="button" aria-describedby={helpId} onClick={() => picker.current?.click()}><Icon kind="plus" />选择照片或视频</button>
    {!!files.length && <ul className="selected-media">
      {files.map(({ file, kind }, index) => <li key={`${file.name}-${file.lastModified}-${file.size}`}>
        <span className="selected-media-icon">{kind === 'photo' ? <SelectedPhoto file={file} index={index+1} /> : <Icon kind="file" />}</span>
        <span className="selected-media-label"><strong>{file.name}</strong><small>{kind === 'photo' ? '照片' : '视频'} · {(file.size / mib).toFixed(1)} MB</small></span>
        <button type="button" className="text-button" aria-label={`移除 ${file.name}`} onClick={() => { setError(''); onChange(files.filter((_, i) => i !== index)); }}>移除</button>
      </li>)}
    </ul>}
    {error && <p className="error" role="alert">{error}</p>}
    {status && <p className="muted" role="status">{status}</p>}
  </section>;
}

function ProtectedMedia({ path, item, name, onPreview }: { path: string; item: MediaEntry; name: string; onPreview: (photo: PreviewPhoto) => void }) {
  const [attempt, setAttempt] = useState(item.kind === 'photo' ? 1 : 0);
  const [url, setURL] = useState('');
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!attempt) return;
    const controller = new AbortController();
    let resource = '';
    const token = sessionStorage.getItem(tokenKey);
    fetch(`${path}/${item.id}`, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: controller.signal, cache: 'no-store' })
      .then(response => {
        if (response.status === 401) {
          sessionStorage.removeItem(tokenKey);
          window.dispatchEvent(new Event('session-expired'));
        }
        if (!response.ok || response.headers.get('Content-Type') !== item.content_type) throw new Error('附件读取失败');
        return response.blob();
      })
      .then(blob => {
        if (controller.signal.aborted) return;
        resource = URL.createObjectURL(blob);
        setURL(resource);
      })
      .catch(() => { if (!controller.signal.aborted) setFailed(true); });
    return () => { controller.abort(); if (resource) URL.revokeObjectURL(resource); };
  }, [path, item.id, item.content_type, attempt]);
  function retry() { setFailed(false); setURL(''); setAttempt(value => value + 1); }

  return <li>
    {failed ? <button type="button" onClick={retry}>重试查看{name}</button> : !attempt ? <button type="button" onClick={retry}>加载{name}</button> :
      !url ? <span role="status">正在读取{name}…</span> :
      item.kind === 'photo' ? <button type="button" className="media-thumbnail" onClick={event => onPreview({ url, name, opener: event.currentTarget })} aria-label={`查看${name}`}><img src={url} alt={name} onError={() => setFailed(true)} /></button> :
      <video src={url} controls playsInline preload="metadata" aria-label={name} onError={() => setFailed(true)} />}
  </li>;
}

function PhotoPreview({ photo, onClose }: { photo: PreviewPhoto; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const frame = dialog.current;
    frame?.showModal();
    return () => { frame?.close(); photo.opener.focus(); };
  }, [photo.opener]);
  return <dialog className="media-preview leave-confirmation" ref={dialog} aria-label={photo.name}
    onCancel={event => { event.preventDefault(); onClose(); }}>
    <header><h2>{photo.name}</h2><button type="button" autoFocus onClick={onClose}>关闭</button></header>
    <img src={photo.url} alt={photo.name} />
  </dialog>;
}

export function OrderMediaGallery({ orderID }: { orderID: number }) {
  const path = `/api/work-orders/${orderID}/media`;
  const [items, setItems] = useState<MediaEntry[]>();
  const [error, setError] = useState(false);
  const [retry, setRetry] = useState(0);
  const [preview, setPreview] = useState<PreviewPhoto>();
  useEffect(() => {
    const controller = new AbortController();
    api<{ items: MediaEntry[] }>(path, { signal: controller.signal })
      .then(payload => { if (!controller.signal.aborted) setItems(payload.items); })
      .catch(() => { if (!controller.signal.aborted) setError(true); });
    return () => controller.abort();
  }, [path, retry]);
  if (!error && !items?.length) return null;
  return <section className="order-media-gallery" aria-labelledby="order-media-heading">
    <h2 id="order-media-heading">现场附件</h2>
    {error ? <p role="alert">附件列表暂时无法读取。 <button type="button" onClick={() => { setError(false); setRetry(value => value+1); }}>重试</button></p> : <ul>
      {items?.map((item, index) => <ProtectedMedia key={item.id} path={path} item={item} name={item.kind === 'photo' ? `照片 ${index+1}` : '视频'} onPreview={setPreview} />)}
    </ul>}
    {preview && <PhotoPreview photo={preview} onClose={() => setPreview(undefined)} />}
  </section>;
}
