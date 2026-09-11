<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const wish = computed(() => extraText(props.msg, 'title') || props.msg.text || '恭喜发财')
const status = computed(() => extraText(props.msg, 'status', '未领取'))
</script>

<template>
  <div class="msg-card msg-pay msg-redpacket">
    <div class="msg-pay-kicker">红包</div>
    <div class="msg-pay-title">{{ wish }}</div>
    <div class="msg-pay-status">{{ status }}</div>
  </div>
</template>
