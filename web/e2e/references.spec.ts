import { test, expect } from '@playwright/test'
const nodes = [
 { id:'n1', type:'process-transform', category:'processing', config:{expression:'payload'}, enabled:true, position:{x:100,y:100} },
 { id:'n2', type:'action-telegram', category:'output', config:{botToken:''}, enabled:true, position:{x:400,y:100} }
]
test.beforeEach(async ({page})=>{
 page.setDefaultTimeout(5000)
 await page.route('**/api/**',route=>{
  const path=new URL(route.request().url()).pathname
  const data=path.endsWith('/auth/status')?{enabled:false}:path.endsWith('/variables')?{variables:{threshold:25,'room-name':'office'}}:path.endsWith('/secrets')?{names:['BOT_TOKEN']}:path.endsWith('/node-types')?{nodeTypes:[
   {type:'process-transform',name:'Transform',category:'processing',config:{properties:{expression:{type:'string',title:'Expression'}}}},
   {type:'action-telegram',name:'Telegram',category:'output',config:{properties:{botToken:{type:'string',title:'Bot Token'},message:{type:'string',title:'Message'}}}}
  ]}:path.endsWith('/flows/ref')?{flow:{id:'ref',name:'References',state:'inactive',nodes,connections:[]},running:false}:{flows:[],runs:[]}
  return route.fulfill({json:{success:true,data}})
 })
})
test('variables manager validates JSON, preserves types, and recovers from a failed save',async({page})=>{
 let calls=0;let saved:any
 await page.route('**/api/variables/limit',route=>{
  calls++;saved=route.request().postDataJSON()
  return route.fulfill(calls===1?{status:500,json:{error:'Unavailable'}}:{json:{success:true}})
 })
 await page.goto('/')
 await page.getByRole('button',{name:'Variables',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'Global variables'})
 await expect(dialog).toContainText('threshold')
 await dialog.getByLabel('Variable name',{exact:true}).fill('limit')
 await dialog.getByLabel('Value format').selectOption('json')
 await dialog.getByLabel('Variable value',{exact:true}).fill('{broken')
 await expect(dialog.getByRole('button',{name:'Save variable'})).toBeDisabled()
 await dialog.getByLabel('Variable value',{exact:true}).fill('false')
 await dialog.getByRole('button',{name:'Save variable'}).click()
 await expect(dialog.getByRole('alert')).toContainText('Could not save')
 await expect(dialog.getByLabel('Variable value',{exact:true})).toHaveValue('false')
 await dialog.getByRole('button',{name:'Save variable'}).click()
 await expect(dialog.getByRole('status')).toContainText('Variable saved')
 expect(saved).toEqual({value:false})
})
test('expression autocomplete inserts a global reference at the caret with keyboard selection',async({page})=>{
 await page.goto('/editor/ref')
 await page.locator('.vue-flow__node').filter({hasText:'Transform'}).click()
 const config=page.getByRole('dialog',{name:'Node configuration'})
 const expression=config.getByLabel('Expression',{exact:true})
 await expression.fill('payload + vars.thr')
 await expect(config.getByRole('option',{name:'threshold',exact:true})).toBeVisible()
 await expression.press('ArrowDown');await expression.press('Enter')
 await expect(expression).toHaveValue('payload + vars["threshold"]')
 await expression.fill('vars.')
 await expression.press('Escape')
 await expect(config.getByRole('listbox')).not.toBeVisible()
 await expect(config).toBeVisible()
})
test('secret names are suggested only in supported credential fields',async({page})=>{
 await page.goto('/editor/ref')
 await page.locator('.vue-flow__node').filter({hasText:'Telegram'}).click()
 const config=page.getByRole('dialog',{name:'Node configuration'})
 const token=config.getByLabel('Bot Token',{exact:true})
 await token.fill('{{secret.BOT')
 await config.getByRole('option',{name:'BOT_TOKEN',exact:true}).click()
 await expect(token).toHaveValue('{{secret.BOT_TOKEN}}')
 await config.getByLabel('Message',{exact:true}).fill('{{secret.')
 await expect(config.getByRole('option',{name:'BOT_TOKEN',exact:true})).not.toBeVisible()
})

test('reference loading can fail and retry without discarding the expression',async({page})=>{
 let fail=true
 await page.route('**/api/variables',route=>fail?route.fulfill({status:500,json:{error:'Offline'}}):route.fallback())
 await page.goto('/editor/ref')
 await page.locator('.vue-flow__node').filter({hasText:'Transform'}).click()
 const config=page.getByRole('dialog',{name:'Node configuration'})
 await expect(config.getByRole('alert')).toContainText('references could not be loaded')
 await config.getByLabel('Expression',{exact:true}).fill('payload + vars.thr')
 fail=false
 await config.getByRole('button',{name:'Retry references'}).click()
 await expect(config.getByRole('alert')).toHaveCount(0)
 await config.getByLabel('Expression',{exact:true}).press('Control+Space')
 await expect(config.getByRole('option',{name:'threshold',exact:true})).toBeVisible()
 await expect(config.getByLabel('Expression',{exact:true})).toHaveValue('payload + vars.thr')
})

test('mobile variable editing and deletion require explicit confirmation',async({page})=>{
 await page.setViewportSize({width:375,height:812})
 let removed=0
 await page.route('**/api/variables/threshold',route=>{if(route.request().method()==='DELETE')removed++;return route.fulfill({json:{success:true}})})
 await page.goto('/')
 await page.getByRole('button',{name:'Variables',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'Global variables'})
 await dialog.getByRole('button',{name:'Edit variable threshold'}).click()
 await expect(dialog.getByLabel('Variable value',{exact:true})).toHaveValue('25')
 await dialog.getByLabel('Variable value',{exact:true}).fill('30')
 await dialog.getByRole('button',{name:'Save variable'}).click()
 await page.getByRole('dialog',{name:'Replace global variable?'}).getByRole('button',{name:'Replace variable',exact:true}).click()
 await expect(dialog.getByRole('status')).toContainText('Variable saved')
 await dialog.getByRole('button',{name:'Delete variable threshold'}).click()
 const confirm=page.getByRole('dialog',{name:'Delete global variable?'})
 await confirm.getByRole('button',{name:'Cancel',exact:true}).click();expect(removed).toBe(0)
 await dialog.getByRole('button',{name:'Delete variable threshold'}).click()
 await confirm.getByRole('button',{name:'Delete variable',exact:true}).click()
 await expect(dialog.getByRole('button',{name:'Edit variable threshold'})).toHaveCount(0)
 expect(removed).toBe(1)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
})
