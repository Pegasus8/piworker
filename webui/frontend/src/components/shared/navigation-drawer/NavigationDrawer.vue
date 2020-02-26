<template>
  <v-navigation-drawer v-model='expandNavDrawer' temporary app>
    <v-list
      nav
      class="py-0"
    >

    <v-list-item two-line class='px-0'>
      <v-list-item-avatar>
        <v-icon x-large dark>mdi-account</v-icon>
      </v-list-item-avatar>

      <v-list-item-content>
        <v-list-item-title class="title">{{ user }}</v-list-item-title>
        <v-list-item-subtitle v-if="isAdmin" class="font-weight-bold">Admin</v-list-item-subtitle>
      </v-list-item-content>
    </v-list-item>

    <v-divider/>

      <v-list-item :to='{ name: "statistics" }' @click="expandNavDrawer = false" class="view-item">
        <v-list-item-icon>
          <v-icon>mdi-chart-areaspline</v-icon>
        </v-list-item-icon>
        <v-list-item-content>
          <v-list-item-title>
            Statistics
          </v-list-item-title>
        </v-list-item-content>
      </v-list-item>

      <v-list-item :to="{ name: 'management' }" @click="expandNavDrawer = false" class="view-item">
        <v-list-item-icon>
          <v-icon>mdi-playlist-edit</v-icon>
        </v-list-item-icon>
        <v-list-item-content>
          <v-list-item-title>
            Management
          </v-list-item-title>
        </v-list-item-content>
      </v-list-item>

      <v-list-item :to="{ name: 'settings' }" @click="expandNavDrawer = false" class="view-item">
        <v-list-item-icon>
          <v-icon>mdi-settings</v-icon>
        </v-list-item-icon>
        <v-list-item-content>
          <v-list-item-title>
            Settings
          </v-list-item-title>
        </v-list-item-content>
      </v-list-item>

      <v-divider/>

      <v-list-item :to="{ name: 'new-task' }" @click="expandNavDrawer = false" class="view-item ">
        <v-list-item-icon>
          <v-icon>mdi-plus-box</v-icon>
        </v-list-item-icon>
        <v-list-item-content>
          <v-list-item-title>
            New Task
          </v-list-item-title>
        </v-list-item-content>
      </v-list-item>

    </v-list>
  </v-navigation-drawer>
</template>
<script>
import { mapGetters } from 'vuex'

export default {
  props: {
    expanded: {
      required: true,
      type: Boolean
    }
  },
  computed: {
    ...mapGetters('auth', [
      'user',
      'isAdmin'
    ]),
    expandNavDrawer: {
      get () {
        return this.expanded
      },
      set (newState) {
        this.$emit('stateChange', newState)
      }
    }
  },
  methods: {
  }
}
</script>
<style lang="scss" scoped>
.view-item {
  text-decoration: none;
}
</style>
