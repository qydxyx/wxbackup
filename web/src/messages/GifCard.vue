<script setup>
import { computed } from 'vue'
import { extraText, mediaURL } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const src = computed(() => extraText(props.msg, 'thumb') || mediaURL(props.msg))
</script>

<template>
  <div class="msg-card msg-card-gif">
    <img v-if="src" class="msg-thumb" :src="src" alt="GIF" />
    <div v-else class="msg-media-missing">GIF 未点开或已过期</div>
  </div>
</template>
