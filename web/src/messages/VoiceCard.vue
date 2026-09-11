<script setup>
import { computed } from 'vue'
import { extraNum, formatDuration, mediaURL } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const src = computed(() => mediaURL(props.msg))
const duration = computed(() => formatDuration(extraNum(props.msg, 'duration')) || extraTextSafe())

function extraTextSafe() {
  return props.msg.text || ''
}
</script>

<template>
  <div class="msg-card msg-voice">
    <span class="msg-voice-bar" />
    <span>{{ duration || '语音' }}</span>
    <audio v-if="src" :src="src" controls />
    <span v-else class="msg-media-missing">未点开</span>
  </div>
</template>
