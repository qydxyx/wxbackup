<script setup>
defineProps({ msg: { type: Object, required: true } })
</script>

<template>
  <div class="msg-card msg-card-text">{{ msg.text || '' }}</div>
</template>
