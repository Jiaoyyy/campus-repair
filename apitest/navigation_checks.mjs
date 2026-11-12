import assert from 'node:assert/strict';
import {parseRoute,routeHash,allowedView,homeView} from '../frontend/src/navigation.ts';
let checks=0;
function equal(actual,expected){assert.deepEqual(actual,expected);checks++;}
for(const role of ['requester','technician','admin']){
 equal(parseRoute('#/orders/42',role),{view:'detail',id:42});
 for(const bad of ['#/orders/0','#/orders/-1','#/orders/1x','#/orders/99999999999999999','#/unknown','#/login','#/orders/42?token=secret'])equal(parseRoute(bad,role),{view:homeView(role)});
 equal(routeHash({view:'detail',id:42,from:'assistant'}),'#/orders/42');
 equal(parseRoute('#/assistant',role),{view:'assistant'});
}
for(const view of ['knowledge','agents','audit']){equal(allowedView(view,'requester'),false);equal(allowedView(view,'technician'),false);equal(parseRoute('#/'+view,'admin'),{view});}
equal(parseRoute('#/create','technician'),{view:'workbench'});equal(parseRoute('#/workbench','requester'),{view:'list'});
for(const role of ['requester','technician','admin']){
 equal(parseRoute('#/orders/42/visit',role),{view:'detail',id:42,section:'visit'});
 equal(routeHash({view:'detail',id:42,section:'visit',from:'workbench'}),'#/orders/42/visit');
 for(const invalid of ['#/orders/0/visit','#/orders/42/visit/extra','#/orders/42/visit?token=x'])equal(parseRoute(invalid,role),{view:homeView(role)});
}
console.log(`Navigation including visit destination passed: ${checks}`);
