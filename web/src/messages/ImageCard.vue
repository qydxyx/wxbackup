<script setup>
import { computed } from 'vue'
import { extraText, mediaURL } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const src = computed(() => extraText(props.msg, 'thumb') || mediaURL(props.msg))
</script>

<template>
  <div class="msg-card msg-card-image">
    <img v-if="src" class="msg-thumb" :src="src" alt="图片" />
    <div v-else class="msg-media-missing">图片未点开或已过期</div>
  </div>
</template>
