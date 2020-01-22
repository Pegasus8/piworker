<template>
  <v-navigation-drawer hide-overlay :value='expanded' app>
  </v-navigation-drawer>
</template>
<script>
export default {
  data () {
    return {
    }
  },
  props: {
    expanded: {
      required: true,
      type: Boolean
    }
  }
}
</script>
<style lang="scss" scoped>

</style>
