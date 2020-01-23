<template>
  <v-navigation-drawer v-model='expandNavDrawer' fixed app>
    <v-list-item-group v-model="activeView">
      <v-list-item :to="{name: 'statistics'}" @click="expandNavDrawer = false">
        <v-list-item-title>
          Statistics
        </v-list-item-title>
      </v-list-item>
      <v-list-item :to="{name: 'management'}" @click="expandNavDrawer = false">
        <v-list-item-title>
          Management
        </v-list-item-title>
      </v-list-item>
      <v-list-item :to="{name: 'settings'}" @click="expandNavDrawer = false">
        <v-list-item-title>
          Settings
        </v-list-item-title>
      </v-list-item>
      <v-list-item :to="{name: 'new-task'}" @click="expandNavDrawer = false">
        <v-list-item-title>
          New Task
        </v-list-item-title>
      </v-list-item>
    </v-list-item-group>
  </v-navigation-drawer>
</template>
<script>
export default {
  data () {
    return {
      activeView: null
    }
  },
  props: {
    expanded: {
      required: true,
      type: Boolean
    }
  },
  computed: {
    expandNavDrawer: {
      get () {
        return this.expanded
      },
      set (newState) {
        this.$emit('stateChange', newState)
      }
    }
  },
  watch: {
    activeView: function (newValue) {
      // When the view is closed close the navigation drawer
      // this.expandNavDrawer = false
    }
  }
}
</script>
<style lang="scss" scoped>

</style>
