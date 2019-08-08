<template>
  <div class="form-group rounded border p-3 my-4">
    <label :for="topElementID" class="text-dark">{{ containerTitle }}</label>
    <slot name="top"></slot>
    <small :id='topElementID + "-description"' class="form-text text-muted text-left">{{ containerDescription }}</small>
    <slot name="bottom"></slot>
  </div>
</template>

<script>
export default {
    props: {
        containerTitle: {
            type: String,
            required: true,
            default: ''
        },
        containerDescription: {
            type: String,
            required: true,
            default: ''
        },
        topElementID: {
          type: String,
          required: true,
          default: ''
        }
    }
}
</script>
