import assert from 'node:assert/strict';
import {streamAnswer, StreamFailure} from '../frontend/src/assistant-api.ts';
const encoder=new TextEncoder();
const meta={conversation_id:7,message_id:42};
const frame=(event,data)=>`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
const start=frame('meta',meta), end=frame('done',{message_id:42,answer:'灯不亮，请核对位置。',citations:[]});
let calls=0,cancelled=0,removed=false,expired=false;
globalThis.sessionStorage={getItem:()=> 'fixture-token',removeItem:()=>{removed=true;}};
globalThis.window={dispatchEvent:()=>{expired=true;}};
function install(chunks, status=200, contentType='text/event-stream') {
 cancelled=0;calls=0;
 globalThis.fetch=async(_url,options)=>{
  calls++;assert.equal(options.headers.Authorization,'Bearer fixture-token');
  return new Response(new ReadableStream({start(c){for(const chunk of chunks)c.enqueue(typeof chunk==='string'?encoder.encode(chunk):chunk);c.close();},cancel(){cancelled++;}}),{status,headers:{'Content-Type':contentType}});
 };
}
async function parse(chunks){install(chunks);const events=[];await streamAnswer(7,'虚构问题',new AbortController().signal,e=>events.push(e));assert.equal(calls,1);return events;}
async function reject(chunks,code='stream_invalid'){
 install(chunks);const events=[];
 await assert.rejects(streamAnswer(7,'虚构问题',new AbortController().signal,e=>events.push(e)),e=>e instanceof StreamFailure && e.code===code);
 assert.equal(calls,1);assert.equal(events.some(e=>e.event==='done'),false);return events;
}
const text=start+frame('phase',{message:'正在核对…'})+frame('delta',{text:'灯不亮。'})+end;
assert.deepEqual((await parse([text])).map(e=>e.event),['meta','phase','delta','done']);
const bytes=encoder.encode(text.replaceAll('\n','\r\n'));
assert.equal((await parse(Array.from(bytes,b=>new Uint8Array([b])))).at(-1).data.answer,'灯不亮，请核对位置。');
await parse([': heartbeat\n\n'+start+frame('future_extension',{ok:true})+end]);
await parse([start,'event: delta\ndata: {"text":\ndata: "分片"}\n\n',end]);
await reject([end]);
await reject([start+start+end]);
await reject([start+end+end]);
await reject([start+end+frame('delta',{text:'不能在完成后追加'})]);
await reject([start+frame('delta',{text:12})+end]);
await reject([start+frame('phase',{message:{}})+end]);
await reject([start+frame('done',{message_id:43,answer:'错误消息',citations:[]})]);
await reject([start+frame('done',{message_id:42,answer:' ',citations:[]})]);
await reject([start+frame('done',{message_id:42,answer:'回答',citations:{}})]);
await reject([frame('meta',{conversation_id:8,message_id:42})+end]);
await reject([frame('meta',{conversation_id:7,message_id:-1})+end]);
await reject([start+'event: delta\ndata: {oops\n\n']);
await reject([start+frame('delta',null)]);
await reject([start,new Uint8Array([255])]);
await reject([start+frame('delta',{text:'未结束'})],'stream_interrupted');
const events=await reject([start+frame('error',{message_id:42,message:'额度不足',code:'query_quota_exhausted'})],'query_quota_exhausted');
assert.equal(events.at(-1).event,'error');
install([JSON.stringify({error:{message:'请重新登录'}})],401,'application/json');
await assert.rejects(streamAnswer(7,'问题',new AbortController().signal,()=>{}),e=>e.status===401);
assert.ok(removed && expired);assert.equal(calls,1);
install(['text'],200,'text/html');await assert.rejects(streamAnswer(7,'问题',new AbortController().signal,()=>{}),StreamFailure);
const abort=new AbortController();abort.abort();calls=0;
globalThis.fetch=async(_url,{signal})=>{calls++;throw signal.reason;};
await assert.rejects(streamAnswer(7,'问题',abort.signal,()=>{}),e=>e.name==='AbortError');assert.equal(calls,1);
console.log('PASS: actual SSE parser handles UTF-8/CRLF splits, lifecycle, malformed/truncated streams, quota, 401 and stop without retry.');
