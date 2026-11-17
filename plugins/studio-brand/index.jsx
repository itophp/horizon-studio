import React from 'react'

const BRAND = 'HorizonStudio'
const ACCENT = '#275ed8'
// Inline HorizonStudio mark: no external asset dependency, survives CSP.
const ICON = 'data:image/svg+xml,<svg xmlns=\\%22http://www.w3.org/2000/svg\\%22 width=\\%221254\\%22 height=\\%221254\\%22 viewBox=\\%220 0 1254 1254\\%22 role=\\%22img\\%22 aria-label=\\%22\\u56db\\u8272\\u6e10\\u53d8\\u65cb\\u8f6c\\u82b1\\u74e3\\u6807\\u5fd7\\%22><defs><linearGradient id=\\%22pink\\%22 x1=\\%2212%\\%22 y1=\\%2286%\\%22 x2=\\%2284%\\%22 y2=\\%2214%\\%22><stop offset=\\%220\\%22 stop-color=\\%22%23df00bd\\%22/><stop offset=\\%220.47\\%22 stop-color=\\%22%23f42a99\\%22/><stop offset=\\%221\\%22 stop-color=\\%22%23ff986a\\%22/></linearGradient><linearGradient id=\\%22purple\\%22 x1=\\%2210%\\%22 y1=\\%220%\\%22 x2=\\%2290%\\%22 y2=\\%22100%\\%22><stop offset=\\%220\\%22 stop-color=\\%22%23b323f7\\%22/><stop offset=\\%220.47\\%22 stop-color=\\%22%239121f3\\%22/><stop offset=\\%221\\%22 stop-color=\\%22%236425e4\\%22/></linearGradient><linearGradient id=\\%22blue\\%22 x1=\\%220%\\%22 y1=\\%220%\\%22 x2=\\%22100%\\%22 y2=\\%22100%\\%22><stop offset=\\%220\\%22 stop-color=\\%22%232023df\\%22/><stop offset=\\%220.5\\%22 stop-color=\\%22%23173ef0\\%22/><stop offset=\\%221\\%22 stop-color=\\%22%230b96fa\\%22/></linearGradient><linearGradient id=\\%22cyan\\%22 x1=\\%220%\\%22 y1=\\%22100%\\%22 x2=\\%22100%\\%22 y2=\\%220%\\%22><stop offset=\\%220\\%22 stop-color=\\%22%2303c5c7\\%22/><stop offset=\\%220.5\\%22 stop-color=\\%22%230fdfc0\\%22/><stop offset=\\%221\\%22 stop-color=\\%22%2316efc1\\%22/></linearGradient></defs><path fill=\\%22url(%23blue)\\%22 d=\\%22M 271,734 L 266,743 L 263,857 L 266,883 L 273,906 L 283,926 L 297,945 L 321,966 L 345,979 L 394,996 L 539,1041 L 590,1054 L 606,1056 L 632,1056 L 649,1053 L 674,1043 L 694,1026 L 702,1013 L 707,994 L 705,975 L 695,951 L 582,724 L 557,688 L 529,662 L 509,650 L 494,644 L 478,640 L 444,639 L 425,643 L 390,657 L 340,683 L 298,710 Z\\%22/><path fill=\\%22url(%23purple)\\%22 d=\\%22M 541,242 L 534,255 L 531,265 L 531,291 L 540,318 L 551,344 L 577,397 L 603,443 L 640,498 L 674,538 L 691,554 L 709,568 L 726,579 L 744,588 L 761,594 L 786,599 L 812,599 L 837,594 L 861,586 L 912,566 L 971,546 L 976,543 L 980,534 L 979,403 L 976,382 L 969,359 L 958,338 L 949,326 L 924,303 L 913,296 L 889,285 L 678,214 L 649,207 L 633,205 L 607,205 L 588,209 L 575,214 L 561,222 Z\\%22/><path fill=\\%22url(%23cyan)\\%22 d=\\%22M 1042,576 L 1028,568 L 1010,564 L 991,565 L 953,576 L 859,618 L 769,655 L 732,673 L 700,694 L 677,716 L 665,733 L 655,757 L 653,768 L 654,793 L 659,810 L 676,850 L 727,957 L 731,961 L 744,962 L 882,933 L 902,926 L 925,914 L 943,900 L 956,886 L 968,869 L 978,850 L 1054,689 L 1063,664 L 1067,645 L 1066,616 L 1057,593 Z\\%22/><path fill=\\%22url(%23pink)\\%22 d=\\%22M 507,306 L 494,306 L 369,341 L 343,350 L 322,361 L 304,374 L 284,394 L 275,406 L 257,440 L 201,590 L 188,629 L 185,651 L 188,677 L 195,694 L 211,712 L 220,717 L 230,720 L 247,720 L 261,715 L 307,678 L 352,649 L 381,633 L 471,589 L 499,573 L 530,551 L 545,537 L 555,525 L 564,511 L 571,494 L 574,479 L 573,456 L 567,436 L 552,399 L 512,311 Z\\%22/></svg>'

// The whole workbench is HorizonStudio: keep the document title, favicons and
// PWA manifest rebranded even if later client code resets them.
function rebrand() {
  if (document.title !== BRAND) document.title = BRAND
  for (const link of document.querySelectorAll('link[rel~="icon"], link[rel="apple-touch-icon"]')) {
    if (link.href !== ICON) link.href = ICON
  }
  const manifest = document.querySelector('link[rel="manifest"]')
  if (manifest) {
    const data = {
      name: BRAND, short_name: BRAND, start_url: '.', display: 'standalone',
      background_color: '#f7f8fa', theme_color: ACCENT,
      icons: [{ src: ICON, sizes: 'any', type: 'image/svg+xml' }],
    }
    const href = 'data:application/manifest+json,' + encodeURIComponent(JSON.stringify(data))
    if (manifest.href !== href) manifest.href = href
  }
}

function Mark({ size = 24 }) {
  return <svg viewBox="0 0 1254 1254" width={size} height={size} aria-hidden="true">
    <defs>
      <linearGradient id="hs-pink" x1="12%" y1="86%" x2="84%" y2="14%"><stop offset="0" stopColor="#df00bd" /><stop offset="0.47" stopColor="#f42a99" /><stop offset="1" stopColor="#ff986a" /></linearGradient>
      <linearGradient id="hs-purple" x1="10%" y1="0%" x2="90%" y2="100%"><stop offset="0" stopColor="#b323f7" /><stop offset="0.47" stopColor="#9121f3" /><stop offset="1" stopColor="#6425e4" /></linearGradient>
      <linearGradient id="hs-blue" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0" stopColor="#2023df" /><stop offset="0.5" stopColor="#173ef0" /><stop offset="1" stopColor="#0b96fa" /></linearGradient>
      <linearGradient id="hs-cyan" x1="0%" y1="100%" x2="100%" y2="0%"><stop offset="0" stopColor="#03c5c7" /><stop offset="0.5" stopColor="#0fdfc0" /><stop offset="1" stopColor="#16efc1" /></linearGradient>
    </defs>
    <path fill="url(#hs-blue)" d="M 271,734 L 266,743 L 263,857 L 266,883 L 273,906 L 283,926 L 297,945 L 321,966 L 345,979 L 394,996 L 539,1041 L 590,1054 L 606,1056 L 632,1056 L 649,1053 L 674,1043 L 694,1026 L 702,1013 L 707,994 L 705,975 L 695,951 L 582,724 L 557,688 L 529,662 L 509,650 L 494,644 L 478,640 L 444,639 L 425,643 L 390,657 L 340,683 L 298,710 Z" />
    <path fill="url(#hs-purple)" d="M 541,242 L 534,255 L 531,265 L 531,291 L 540,318 L 551,344 L 577,397 L 603,443 L 640,498 L 674,538 L 691,554 L 709,568 L 726,579 L 744,588 L 761,594 L 786,599 L 812,599 L 837,594 L 861,586 L 912,566 L 971,546 L 976,543 L 980,534 L 979,403 L 976,382 L 969,359 L 958,338 L 949,326 L 924,303 L 913,296 L 889,285 L 678,214 L 649,207 L 633,205 L 607,205 L 588,209 L 575,214 L 561,222 Z" />
    <path fill="url(#hs-cyan)" d="M 1042,576 L 1028,568 L 1010,564 L 991,565 L 953,576 L 859,618 L 769,655 L 732,673 L 700,694 L 677,716 L 665,733 L 655,757 L 653,768 L 654,793 L 659,810 L 676,850 L 727,957 L 731,961 L 744,962 L 882,933 L 902,926 L 925,914 L 943,900 L 956,886 L 968,869 L 978,850 L 1054,689 L 1063,664 L 1067,645 L 1066,616 L 1057,593 Z" />
    <path fill="url(#hs-pink)" d="M 507,306 L 494,306 L 369,341 L 343,350 L 322,361 L 304,374 L 284,394 L 275,406 L 257,440 L 201,590 L 188,629 L 185,651 L 188,677 L 195,694 L 211,712 L 220,717 L 230,720 L 247,720 L 261,715 L 307,678 L 352,649 L 381,633 L 471,589 L 499,573 L 530,551 L 545,537 L 555,525 L 564,511 L 571,494 L 574,479 L 573,456 L 567,436 L 552,399 L 512,311 Z" />
  </svg>
}

export const name = 'horizon-studio-brand'
export const inject = ['slots']

export function apply(ctx) {
  // The shell renders its own logo and product name through these slots; taking
  // them over replaces every remaining DSH wordmark outside the plugin panels.
  ctx.slots.inject('sidebar.brand.mark', function* () {
    yield ctx.slots.register({ name: 'sidebar.brand.mark', key: 'horizon-studio' }, () => <Mark />)
  })
  ctx.slots.inject('sidebar.brand.name', function* () {
    yield ctx.slots.register({ name: 'sidebar.brand.name', key: 'horizon-studio' },
      () => <strong style={{ letterSpacing: '-0.2px' }}>{BRAND}</strong>)
  })
  // DSH renders its own dock panels (document preview, terminal) on the right.
  // Inside HorizonStudio those duplicate the shell's own panes, so the agent
  // surface is reduced to the conversation column only.
  const style = document.createElement('style')
  style.dataset.horizonstudio = 'agent-surface'
  style.textContent = `
    /* DSH's own columns and session header duplicate the HorizonStudio frame:
       keep the conversation column only. The outer frame is a CSS grid, so the
       collapsed sidebar/right columns must also be removed from the track list,
       otherwise their widths survive as empty gutters. */
    [class*="sidebarCol"], [class*="rightbarCol"], [class*="leadingSeat"], [class*="drawerMask"] { display: none !important; }
    [class*="frame"] { grid-template-columns: 0 minmax(0, 1fr) 0 !important; }
    [class*="mainPanel"] header { display: none !important; }
    [data-dockkit-host="dock"] { display: none !important; }
  `
  document.head.append(style)
  ctx.effect(() => () => style.remove(), 'agent surface: hide DSH dock panels')
  rebrand()
  const observer = new MutationObserver(rebrand)
  observer.observe(document.head, { childList: true, subtree: true, characterData: true })
  ctx.effect(() => () => observer.disconnect(), 'brand: keep document rebranded')
}
