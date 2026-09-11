<script setup>
import { computed } from 'vue'
import { extraText, mediaURL } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const src = computed(() => extraText(props.msg, 'thumb') || mediaURL(props.msg))
</script>

<template>
  <div class="msg-card msg-card-emoji">
    <img v-if="src" class="msg-thumb" :src="src" alt="表情" />
    <div v-else>{{ msg.text || '[表情]' }}</div>
  </div>
</template>
