<script setup>
import { computed } from 'vue'
import { extraNum, extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const title = computed(() => extraText(props.msg, 'title') || props.msg.text || '聊天记录')
const count = computed(() => extraNum(props.msg, 'count'))
</script>

<template>
  <div class="msg-card msg-card-merge">
    <div class="msg-card-kicker">合并转发</div>
    <div class="msg-card-title">{{ title }}</div>
    <div v-if="count" class="msg-card-desc">共 {{ count }} 条</div>
  </div>
</template>
