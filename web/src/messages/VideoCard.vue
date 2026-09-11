<script setup>
import { computed } from 'vue'
import { extraText, mediaURL } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const src = computed(() => mediaURL(props.msg))
const poster = computed(() => extraText(props.msg, 'thumb'))
</script>

<template>
  <div class="msg-card msg-card-video">
    <video v-if="src" class="msg-thumb" :src="src" :poster="poster || undefined" controls />
    <div v-else class="msg-media-missing">{{ msg.text || '视频未点开或已过期' }}</div>
  </div>
</template>
