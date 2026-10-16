// Development only: export the approved preview's monochrome Lucide masks.
// Usage: NODE_PATH=<playwright modules> node scripts/export-crt-icons.cjs <preview.html>
const {chromium}=require('playwright');
const fs=require('node:fs');
const path=require('node:path');
const {pathToFileURL}=require('node:url');
let browser;
(async()=>{
  if(!process.argv[2])throw Error('Pass the approved CRT preview HTML path');
  browser=await chromium.launch({headless:true});
  const page=await browser.newPage();
  await page.goto(pathToFileURL(path.resolve(process.argv[2])).href);
  const frame=page.frames().find(frame=>frame!==page.mainFrame());
  await frame.waitForFunction(()=>{
    const icons=[...document.querySelectorAll('.crt-icon')];
    return icons.length>15&&icons.every(icon=>icon.classList.contains('crt-pixel-ready'));
  });
  const icons=await frame.evaluate(()=>{
    const result={};
    document.querySelectorAll('.crt-icon').forEach(icon=>{
      const name=icon.querySelector('svg').getAttribute('data-lucide');
      if(result[name])return;
      const source=icon.querySelector('canvas'), canvas=document.createElement('canvas');
      canvas.width=16;canvas.height=16;
      const context=canvas.getContext('2d');
      const pixels=source.getContext('2d').getImageData(0,0,16,16);
      for(let i=0;i<pixels.data.length;i+=4){pixels.data[i]=255;pixels.data[i+1]=255;pixels.data[i+2]=255;}
      context.putImageData(pixels,0,0);result[name]=canvas.toDataURL('image/png').split(',')[1];
    });
    return result;
  });
  const directory=path.resolve(__dirname,'../frontend/icons');fs.mkdirSync(directory,{recursive:true});
  for(const [name,data] of Object.entries(icons))fs.writeFileSync(path.join(directory,name+'.png'),Buffer.from(data,'base64'));
  const appIcon=await frame.evaluate(async data=>{
    const image=new Image();image.src='data:image/png;base64,'+data;await image.decode();
    const canvas=document.createElement('canvas');canvas.width=256;canvas.height=256;
    const context=canvas.getContext('2d');context.imageSmoothingEnabled=false;
    context.fillStyle='#09130d';context.fillRect(0,0,256,256);
    context.strokeStyle='#2b4631';context.lineWidth=8;context.strokeRect(12,12,232,232);
    context.drawImage(image,64,64,128,128);context.globalCompositeOperation='source-atop';
    // Color only the exported white glyph; the surrounding glass remains dark.
    const pixels=context.getImageData(0,0,256,256);
    for(let i=0;i<pixels.data.length;i+=4)if(pixels.data[i]===255&&pixels.data[i+1]===255&&pixels.data[i+2]===255){pixels.data[i]=151;pixels.data[i+1]=234;pixels.data[i+2]=174;}
    context.putImageData(pixels,0,0);return canvas.toDataURL('image/png').split(',')[1];
  },icons.network);
  fs.writeFileSync(path.resolve(__dirname,'../build/appicon.png'),Buffer.from(appIcon,'base64'));
  console.log('Exported '+Object.keys(icons).length+' local pixel icons and the application icon.');
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(async()=>{if(browser)await browser.close();});
