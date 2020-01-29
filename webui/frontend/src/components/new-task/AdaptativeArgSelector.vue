<template>
<v-text-field
  v-model.lazy='argContent'
  :rules='[
    v => !!v || "Empty args are not allowed"
  ]'
  required
/>
</template>

<script>
export default {
  props: {
    content: {
      type: String,
      required: true
    },
    argType: {
      type: String,
      required: true
    }
  },
  computed: {
    argContent: {
      get () {
        return this.content
      },
      set (newValue) {
        this.$emit('changed', newValue)
      }
    }
  },
  mounted () {
    console.log(this.content)
  }
}
</script>