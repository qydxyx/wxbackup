<script setup>
defineProps({ msg: { type: Object, required: true } })
</script>

<template>
  <div class="msg-card msg-card-system">{{ msg.text || '[系统]' }}</div>
</template>
