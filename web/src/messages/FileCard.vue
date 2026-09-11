<script setup>
import { computed } from 'vue'
import { extraNum, extraText, formatSize } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const name = computed(() => extraText(props.msg, 'filename') || extraText(props.msg, 'title') || props.msg.text || '文件')
const size = computed(() => formatSize(extraNum(props.msg, 'size')))
</script>

<template>
  <div class="msg-card msg-card-file">
    <div class="msg-card-kicker">文件</div>
    <div class="msg-card-title">{{ name }}</div>
    <div v-if="size" class="msg-card-desc">{{ size }}</div>
  </div>
</template>
