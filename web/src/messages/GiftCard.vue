<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const name = computed(() => extraText(props.msg, 'title') || extraText(props.msg, 'name') || props.msg.text || '礼物')
</script>

<template>
  <div class="msg-card msg-card-gift">
    <div class="msg-card-kicker">礼物</div>
    <div class="msg-card-title">{{ name }}</div>
  </div>
</template>
