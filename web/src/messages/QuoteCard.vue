<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const quoted = computed(() => extraText(props.msg, 'quoted_text') || extraText(props.msg, 'refer'))
</script>

<template>
  <div class="msg-card msg-card-quote">
    <div v-if="quoted" class="msg-quote-ref">{{ quoted }}</div>
    <div>{{ msg.text || '[引用]' }}</div>
  </div>
</template>
