import { createApp } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import { formatTime, kindLabel, loginStateLabel } from './format'
import { CATALOG, Kind, MsgType } from './types'
import { isSystem, isUnsupported, messagePreview, resolveKind } from './resolve'
import { cardFor, CARDS } from './registry'
import { extraText, formatDuration, formatSize, mediaURL } from './extra'
import MessageItem from './MessageItem.vue'
import PlaceholderCard from './PlaceholderCard.vue'

const mounts = []

function render(msg) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp(MessageItem, { msg })
  app.mount(el)
  mounts.push({ app, el })
  return el
}

afterEach(() => {
  while (mounts.length) {
    const { app, el } = mounts.pop()
    app.unmount()
    el.remove()
  }
})

describe('messagePreview', () => {
  it('uses text for type 1 and placeholders for others', () => {
    expect(messagePreview({ msg_type: 1, text: 'hello fixture' })).toBe('hello fixture')
    expect(messagePreview({ msg_type: 3 })).toBe('[图片]')
    expect(messagePreview({ msg_type: 34 })).toBe('[语音]')
    expect(messagePreview({ msg_type: 43 })).toBe('[视频]')
    expect(messagePreview({ msg_type: 10000, text: 'synthetic system notice' })).toBe(
      'synthetic system notice',
    )
  })

  it('covers catalog kinds', () => {
    expect(messagePreview({ msg_type: MsgType.File })).toBe('[文件]')
    expect(messagePreview({ msg_type: MsgType.Link })).toBe('[链接]')
    expect(messagePreview({ msg_type: 49, extra: { kind: 'redpacket' } })).toBe('[红包]')
    expect(messagePreview({ msg_type: MsgType.AVCall })).toBe('[音视频通话]')
    expect(messagePreview({ msg_type: MsgType.OANotify })).toBe('[公众号通知]')
    expect(messagePreview({ msg_type: 9999 })).toBe('[消息]')
  })
})

describe('resolveKind', () => {
  it('maps integers, extra.kind, and extra.app_type', () => {
    expect(resolveKind({ msg_type: 1 })).toBe(Kind.text)
    expect(resolveKind({ msg_type: 49, extra: { kind: 'miniprogram' } })).toBe(Kind.miniprogram)
    expect(resolveKind({ msg_type: 49, extra: { app_type: MsgType.Quote } })).toBe(Kind.quote)
    expect(resolveKind({ msg_type: 50 })).toBe(Kind.avcall)
    expect(isSystem({ msg_type: 10000 })).toBe(true)
    expect(isUnsupported({ msg_type: 50 })).toBe(true)
    expect(isUnsupported({ msg_type: 1 })).toBe(false)
  })
})

describe('catalog', () => {
  it('has 21 supported kinds each with a card', () => {
    const supported = CATALOG.filter((i) => i.supported)
    expect(supported).toHaveLength(21)
    for (const info of supported) {
      expect(CARDS[info.kind], info.kind).toBeTruthy()
    }
    expect(cardFor(Kind.oanotify)).toBe(PlaceholderCard)
    expect(cardFor(Kind.avcall)).toBe(PlaceholderCard)
    expect(cardFor(Kind.unknown)).toBe(PlaceholderCard)
  })
})

describe('MessageItem matrix', () => {
  it('renders a card for every catalog kind', () => {
    for (const info of CATALOG) {
      const el = render({
        msg_id: info.kind,
        msg_type: info.code,
        text: info.label,
        extra: { title: info.label, filename: 'a.txt', quoted_text: 'q' },
        create_time: '2024-01-02T03:04:00Z',
      })
      const text = el.textContent || ''
      if (info.supported) {
        expect(text, info.kind).not.toContain('暂不支持')
      } else {
        expect(text, info.kind).toContain('暂不支持')
      }
    }
  })

  it('shows unopened red packet copy', () => {
    const el = render({ msg_type: MsgType.RedPacket, extra: { title: '恭喜发财' } })
    expect(el.textContent).toContain('红包')
    expect(el.textContent).toContain('未领取')
  })

  it('marks system rows', () => {
    const el = render({ msg_type: 10000, text: 'synthetic system notice' })
    expect(el.querySelector('.bubble-row.sys')).toBeTruthy()
    expect(el.textContent).toContain('synthetic system notice')
  })
})

describe('helpers', () => {
  it('formats time, sizes, and media urls', () => {
    expect(formatTime('2024-01-02T03:04:00Z')).toMatch(/2024-01-02/)
    expect(loginStateLabel('logged_in')).toBe('已登录')
    expect(kindLabel('group')).toBe('群')
    expect(formatSize(2048)).toBe('2.0 KB')
    expect(formatDuration(65)).toBe('1:05')
    expect(extraText({ extra: { title: 't' } }, 'title')).toBe('t')
    expect(mediaURL({ account_id: 'a1', extra: { media_id: 'media_available' } })).toBe(
      '/v1/media/media_available?account_id=a1',
    )
  })
})
