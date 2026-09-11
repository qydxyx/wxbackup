<script setup>
import { computed } from 'vue'
import { extraText } from './extra'

const props = defineProps({ msg: { type: Object, required: true } })
const title = computed(() => extraText(props.msg, 'title') || props.msg.text || '群接龙')
const desc = computed(() => extraText(props.msg, 'desc'))
</script>

<template>
  <div class="msg-card msg-card-groupchain">
    <div class="msg-card-kicker">群接龙</div>
    <div class="msg-card-title">{{ title }}</div>
    <div v-if="desc" class="msg-card-desc">{{ desc }}</div>
  </div>
</template>
