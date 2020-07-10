<template>
  <v-row justify='center'>
    <v-dialog v-model='showDialog' max-width="600px" scrollable>
      <v-card>
        <v-card-title>
          Choose a{{ elementType === 'action'? 'n':'' }} {{ elementType }}
        </v-card-title>
        <v-card-text>
          <v-expansion-panels inset>
            <v-expansion-panel v-for='element in elements' :key='element.ID'>
              <v-expansion-panel-header :disable-icon-rotate='elementSelected === element'>
                {{ element.name }}
                <template v-if='elementSelected === element' v-slot:actions>
                  <v-icon color="blue darken-2">mdi-check-circle</v-icon>
                </template>
              </v-expansion-panel-header>
              <v-expansion-panel-content>
                <v-card :elevation='0'>
                  <v-card-text>
                    {{ element.description }}
                  </v-card-text>
                  <v-card-actions>
                    <v-spacer/>
                    <v-fade-transition>
                      <v-btn
                        v-if='elementSelected !== element'
                        color='green'
                        @click='elementSelected = element'
                        text
                        icon
                      >
                        <v-icon>mdi-plus</v-icon>
                      </v-btn>
                      <v-btn
                        v-else
                        color='red'
                        @click='elementSelected = null'
                        text
                        icon
                      >
                        <v-icon>mdi-close</v-icon>
                      </v-btn>
                    </v-fade-transition>
                  </v-card-actions>
                </v-card>
              </v-expansion-panel-content>
            </v-expansion-panel>
          </v-expansion-panels>
        </v-card-text>
        <v-card-actions>
          <v-spacer/>
          <v-btn color='primary darken-2' @click="submit" :disabled='!elementSelected'>
            Add
          </v-btn>
          <v-btn color='red darken-2' @click="showDialog = false">
            Dismiss
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </v-row>
</template>

<script>
export default {
  data () {
    return {
      elementSelected: null
    }
  },
  props: {
    elementType: {
      type: String,
      required: true
    },
    elements: {
      type: Array,
      required: true
    },
    show: {
      type: Boolean,
      required: true
    }
  },
  computed: {
    showDialog: {
      get () {
        return this.show
      },
      set (newValue) {
        this.$emit('dismissed')
      }
    }
  },
  methods: {
    submit () {
      this.$emit('elementSelected', this.elementSelected)
      this.showDialog = false
      this.elementSelected = null
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
