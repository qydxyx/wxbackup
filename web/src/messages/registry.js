import { Kind } from './types'
import TextCard from './TextCard.vue'
import ImageCard from './ImageCard.vue'
import VideoCard from './VideoCard.vue'
import VoiceCard from './VoiceCard.vue'
import FileCard from './FileCard.vue'
import LinkCard from './LinkCard.vue'
import MiniProgramCard from './MiniProgramCard.vue'
import CardCard from './CardCard.vue'
import LocationCard from './LocationCard.vue'
import GifCard from './GifCard.vue'
import EmojiCard from './EmojiCard.vue'
import QuoteCard from './QuoteCard.vue'
import MergeCard from './MergeCard.vue'
import RedPacketCard from './RedPacketCard.vue'
import TransferCard from './TransferCard.vue'
import SystemCard from './SystemCard.vue'
import ChannelsCard from './ChannelsCard.vue'
import LiveCard from './LiveCard.vue'
import GroupChainCard from './GroupChainCard.vue'
import GroupNoticeCard from './GroupNoticeCard.vue'
import GiftCard from './GiftCard.vue'
import PlaceholderCard from './PlaceholderCard.vue'

export const CARDS = {
  [Kind.text]: TextCard,
  [Kind.image]: ImageCard,
  [Kind.video]: VideoCard,
  [Kind.voice]: VoiceCard,
  [Kind.file]: FileCard,
  [Kind.link]: LinkCard,
  [Kind.miniprogram]: MiniProgramCard,
  [Kind.card]: CardCard,
  [Kind.location]: LocationCard,
  [Kind.gif]: GifCard,
  [Kind.emoji]: EmojiCard,
  [Kind.quote]: QuoteCard,
  [Kind.merge]: MergeCard,
  [Kind.redpacket]: RedPacketCard,
  [Kind.transfer]: TransferCard,
  [Kind.system]: SystemCard,
  [Kind.channels]: ChannelsCard,
  [Kind.live]: LiveCard,
  [Kind.groupchain]: GroupChainCard,
  [Kind.groupnotice]: GroupNoticeCard,
  [Kind.gift]: GiftCard,
}

export function cardFor(kind) {
  return CARDS[kind] || PlaceholderCard
}
